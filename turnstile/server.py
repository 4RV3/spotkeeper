from flask import Flask, jsonify, request
from context_api import ConfigError, allowed_host, parse_context, TelemetryReceipt, release_when_done
from camoufox.sync_api import Camoufox
import time
import os
import subprocess
import traceback
import threading
import queue

app = Flask(__name__)
app.config["MAX_CONTENT_LENGTH"] = 8192

PORT = int(os.environ.get("PORT", 3099))
WORKERS = int(os.environ.get("WORKERS", 1))


def default_turnstile_url():
    """Warm-up page URL, built from API_DOMAIN."""
    return f"https://{allowed_host()}/turnstile?v=1"


def window_title_match():
    """Substring of the browser window title for xdotool: env WINDOW_TITLE_MATCH,
    default the first label of API_DOMAIN."""
    override = os.environ.get("WINDOW_TITLE_MATCH", "").strip()
    return override or allowed_host().split(".")[0]


TIMEOUT_S = int(os.environ.get("TIMEOUT_S", 60))
POOL_ACQUIRE_TIMEOUT_S = float(os.environ.get("POOL_ACQUIRE_TIMEOUT_S", 120))
REUSE_BROWSER = os.environ.get("REUSE_BROWSER", "1").lower() in ("1", "true", "yes")
WARM_PAGE = os.environ.get("WARM_PAGE", "0").lower() in ("1", "true", "yes")
CHALLENGE_ROUTE = "**/challenges.cloudflare.com/**"

INIT_SCRIPT = """
window.__turnstileResult = null;
window.ReactNativeWebView = {
    postMessage: function(data) {
        try {
            window.__turnstileResult = JSON.parse(data);
        } catch(e) {
            window.__turnstileResult = { raw: data };
        }
    }
};
"""


# --- Worker pool: each worker owns a dedicated Xvfb display ---

class WorkerPool:
    """Pool of workers, each with its own Xvfb virtual display.

    Concurrent GET /token requests are dispatched to free workers.
    If all workers are busy, requests queue up and wait.
    """

    def __init__(self, size):
        self.size = size
        self.queue = queue.Queue()
        self._xvfb_procs = {}
        self._lock = threading.Lock()

        for i in range(size):
            display = 100 + i
            self.ensure_xvfb(display)
            self.queue.put(display)
            print(f"[POOL] Worker {i} ready on display :{display}")

    def _display_env(self, display):
        return {**os.environ, "DISPLAY": f":{display}"}

    def _display_is_ready(self, display):
        try:
            result = subprocess.run(
                ["xdotool", "getdisplaygeometry"],
                capture_output=True,
                text=True,
                timeout=3,
                env=self._display_env(display),
            )
            if result.returncode == 0:
                return True
            print(f"[POOL] Display :{display} check failed: {result.stderr.strip()}")
        except Exception as e:
            print(f"[POOL] Display :{display} check failed: {e}")
        return False

    def _cleanup_display_files(self, display):
        for path in (f"/tmp/.X{display}-lock", f"/tmp/.X11-unix/X{display}"):
            try:
                os.unlink(path)
                print(f"[POOL] Removed stale X11 file {path}")
            except FileNotFoundError:
                pass
            except Exception as e:
                print(f"[POOL] Could not remove {path}: {e}")

    def _start_xvfb(self, display):
        self._cleanup_display_files(display)
        proc = subprocess.Popen(
            ['Xvfb', f':{display}', '-screen', '0', '1920x1080x24',
             '-ac', '-nolisten', 'tcp'],
            stdout=subprocess.DEVNULL,
            stderr=subprocess.PIPE,
            text=True,
        )
        self._xvfb_procs[display] = proc

        deadline = time.time() + 5
        while time.time() < deadline:
            if proc.poll() is not None:
                err = proc.stderr.read() if proc.stderr else ""
                raise RuntimeError(f"Xvfb :{display} exited during startup: {err.strip()}")
            if self._display_is_ready(display):
                return proc
            time.sleep(0.2)

        self._stop_xvfb(display)
        raise RuntimeError(f"Xvfb :{display} did not become ready")

    def _stop_xvfb(self, display):
        proc = self._xvfb_procs.pop(display, None)
        if not proc:
            self._cleanup_display_files(display)
            return
        if proc.poll() is None:
            proc.terminate()
            try:
                proc.wait(timeout=3)
            except subprocess.TimeoutExpired:
                proc.kill()
                proc.wait(timeout=3)
        else:
            try:
                proc.wait(timeout=1)
            except Exception:
                pass
        self._cleanup_display_files(display)

    def ensure_xvfb(self, display):
        with self._lock:
            proc = self._xvfb_procs.get(display)
            if proc and proc.poll() is None and self._display_is_ready(display):
                return True

            if proc:
                print(f"[POOL] Restarting dead display :{display}")
                self._stop_xvfb(display)

            self._start_xvfb(display)
            return True

    def acquire(self, timeout=60):
        """Get a free display number. Blocks until one is available."""
        try:
            display = self.queue.get(timeout=timeout)
            self.ensure_xvfb(display)
            return display
        except queue.Empty:
            return None

    def release(self, display):
        """Return display to pool."""
        try:
            self.ensure_xvfb(display)
        except Exception as e:
            print(f"[POOL] Display :{display} restart failed before release: {e}")
        self.queue.put(display)

    @property
    def available(self):
        return self.queue.qsize()

    def health(self):
        displays = {}
        ok = True
        with self._lock:
            for display in range(100, 100 + self.size):
                proc = self._xvfb_procs.get(display)
                running = bool(proc and proc.poll() is None)
                ready = running and self._display_is_ready(display)
                displays[f":{display}"] = {"running": running, "ready": ready}
                ok = ok and ready
        return ok, displays


pool = None  # initialized in main
CONTEXT_REQUEST_TIMEOUT_S = float(os.environ.get("CONTEXT_REQUEST_TIMEOUT_S", 100))


# --- X11 helpers (display-aware) ---

def find_browser_window(display):
    env = {**os.environ, "DISPLAY": f":{display}"}
    try:
        title_match = window_title_match()
    except ConfigError:
        title_match = 'Verification'
    for search in [
        ['xdotool', 'search', '--name', 'Verification'],
        ['xdotool', 'search', '--name', title_match],
        ['xdotool', 'search', '--class', 'firefox'],
        ['xdotool', 'search', '--class', 'Navigator'],
    ]:
        result = subprocess.run(search, capture_output=True, text=True, timeout=5, env=env)
        ids = [w.strip() for w in result.stdout.strip().split('\n') if w.strip()]
        if ids:
            return ids[-1]

    result = subprocess.run(
        ['xdotool', 'getactivewindow'],
        capture_output=True, text=True, timeout=5, env=env
    )
    wid = result.stdout.strip()
    return wid if wid else None


def get_checkbox_coords(page, display):
    try:
        env = {**os.environ, "DISPLAY": f":{display}"}
        win_id = find_browser_window(display)
        if not win_id:
            return None

        geo = subprocess.run(
            ['xdotool', 'getwindowgeometry', '--shell', win_id],
            capture_output=True, text=True, timeout=5, env=env
        )
        win = {}
        for line in geo.stdout.strip().split('\n'):
            if '=' in line:
                k, v = line.split('=', 1)
                try: win[k] = int(v)
                except ValueError: win[k] = v

        win_h = win.get('HEIGHT', 0)
        viewport = page.evaluate("() => ({w: window.innerWidth, h: window.innerHeight})")
        toolbar_h = max(0, win_h - viewport['h'])

        elem = page.evaluate("""() => {
            const el = document.querySelector('.cf-turnstile');
            if (el) { const r = el.getBoundingClientRect(); return {x:r.x, y:r.y, w:r.width, h:r.height}; }
            const iframe = document.querySelector('iframe[src*="challenges.cloudflare.com"]');
            if (iframe) { const r = iframe.getBoundingClientRect(); return {x:r.x, y:r.y, w:r.width, h:r.height}; }
            return null;
        }""")
        if not elem:
            return None

        cb_x = int(elem['x']) + 28
        cb_y = toolbar_h + int(elem['y']) + int(elem['h'] / 2)
        print(f"[W{display}] Checkbox at window-relative ({cb_x}, {cb_y}), "
              f"toolbar={toolbar_h}px, elem=({elem['x']:.0f},{elem['y']:.0f})")
        return win_id, cb_x, cb_y

    except Exception as e:
        print(f"[W{display}] get_checkbox_coords failed: {e}")
        return None


def xdotool_click(display, win_id, x, y):
    ix, iy = int(x), int(y)
    env = {**os.environ, "DISPLAY": f":{display}"}

    subprocess.run(['xdotool', 'windowfocus', '--sync', win_id], timeout=5, env=env)
    time.sleep(0.3)
    subprocess.run(['xdotool', 'mousemove', '--window', win_id, '--',
                    str(ix + 60), str(iy + 30)], timeout=5, env=env)
    time.sleep(0.15)
    subprocess.run(['xdotool', 'mousemove', '--window', win_id, '--',
                    str(ix + 10), str(iy + 5)], timeout=5, env=env)
    time.sleep(0.1)
    subprocess.run(['xdotool', 'mousemove', '--window', win_id, '--',
                    str(ix), str(iy)], timeout=5, env=env)
    time.sleep(0.12)
    subprocess.run(['xdotool', 'click', '1'], timeout=5, env=env)
    print(f"[W{display}] Clicked at ({ix}, {iy})")


def extract_token(page, display):
    result = page.evaluate("() => window.__turnstileResult")
    if result and result.get("isSuccess") and result.get("token"):
        print(f"[W{display}] Token from postMessage ({len(result['token'])} chars)")
        return result["token"]

    token = page.evaluate("""() => {
        const input = document.querySelector('input[name="cf-turnstile-response"]');
        return input ? input.value : null;
    }""")
    if token and len(token) > 20:
        print(f"[W{display}] Token from hidden input ({len(token)} chars)")
        return token

    token = page.evaluate("""() => {
        try { if (window.turnstile) return window.turnstile.getResponse(); } catch(e) {}
        return null;
    }""")
    if token and len(token) > 20:
        print(f"[W{display}] Token from getResponse() ({len(token)} chars)")
        return token

    return None


# --- Core token generation (runs on a specific display) ---

_display_lock = threading.Lock()


def get_token_on_display(display, context=None, done=None):
    """Run token generation in a fresh thread to avoid Playwright asyncio conflicts.

    Playwright sync API uses greenlets and leaves asyncio event loop state in the
    thread. When Waitress reuses that thread, Playwright detects the stale running
    loop and raises. A fresh thread guarantees clean asyncio state every time.
    """
    result_box = {}

    def _worker():
        try:
            result_box["value"] = _get_token_impl(display, context=context)
        except Exception as e:
            result_box["error"] = e
        finally:
            if done is not None:
                done.set()

    thread = threading.Thread(target=_worker, daemon=True)
    thread.start()
    thread.join(timeout=CONTEXT_REQUEST_TIMEOUT_S if context else TIMEOUT_S + 60)

    if "error" in result_box:
        raise result_box["error"]
    if "value" not in result_box:
        return {"ok": False, "error": "Worker thread timed out"}
    return result_box["value"]


def _solve_loaded_page(page, display, start=None):
    if start is None:
        start = time.time()
    tag = f"[W{display}]"

    try:
        page.wait_for_load_state("networkidle", timeout=5000)
    except Exception as e:
        print(f"{tag} Networkidle wait skipped: {type(e).__name__}")
    print(f"{tag} Page loaded in {time.time()-start:.1f}s")

    # Quick check for auto-verification before clicking
    page.wait_for_timeout(300)
    token = extract_token(page, display)
    if token:
        elapsed = time.time() - start
        print(f"{tag} Auto-verified in {elapsed:.1f}s")
        return {"ok": True, "token": token, "elapsed": round(elapsed, 1)}

    # Click the checkbox immediately
    coords = get_checkbox_coords(page, display)
    if coords:
        win_id, cb_x, cb_y = coords
        xdotool_click(display, win_id, cb_x, cb_y)
        print(f"{tag} Clicked at {time.time()-start:.1f}s")

    # Poll frequently for token
    poll_interval = 1
    click_attempts = 1
    max_wait = TIMEOUT_S - int(time.time() - start)

    for waited in range(0, max(max_wait, 1), poll_interval):
        token = extract_token(page, display)
        if token:
            elapsed = time.time() - start
            print(f"{tag} Token after {elapsed:.1f}s")
            return {"ok": True, "token": token, "elapsed": round(elapsed, 1)}

        result = page.evaluate("() => window.__turnstileResult")
        if result and not result.get("isSuccess") and result.get("reason"):
            return {"ok": False, "reason": result.get("reason", str(result))}

        page.wait_for_timeout(poll_interval * 1000)

        # Retry click every 8 seconds
        if waited > 0 and waited % 8 == 0 and click_attempts < 4:
            click_attempts += 1
            print(f"{tag} Retry click #{click_attempts}")
            coords = get_checkbox_coords(page, display)
            if coords:
                xdotool_click(display, coords[0], coords[1], coords[2])

    elapsed = time.time() - start
    print(f"{tag} Timeout after {elapsed:.1f}s")
    return {"ok": False, "error": "Turnstile timeout", "elapsed": round(elapsed, 1)}


def _solve_with_browser(browser, display, start=None, context=None):
    if start is None:
        start = time.time()

    page = browser.new_page()
    try:
        page.add_init_script(INIT_SCRIPT)
        receipt = TelemetryReceipt(context) if context else None
        if receipt:
            page.on("response", receipt.response)
        page.goto(context.url if context else default_turnstile_url(), wait_until="domcontentloaded", timeout=15000)
        result = _solve_loaded_page(page, display, start=start)
        return receipt.finish(page, result) if receipt else result
    finally:
        try:
            page.close()
        except Exception:
            pass


def _get_token_impl(display, context=None):
    start = time.time()
    tag = f"[W{display}]"
    print(f"{tag} Starting token generation")
    browser_ctx = None

    # Set DISPLAY atomically for browser launch (Camoufox inherits os.environ)
    with _display_lock:
        os.environ["DISPLAY"] = f":{display}"
        browser_ctx = Camoufox(
            headless=False,
            humanize=True,
            os="linux",
            disable_coop=True,
            window=(1280, 720),
        )
        browser = browser_ctx.__enter__()

    try:
        return _solve_with_browser(browser, display, start=start, context=context)

    finally:
        if browser_ctx:
            try:
                browser_ctx.__exit__(None, None, None)
            except Exception:
                pass


class WarmBrowserWorker:
    def __init__(self, display_pool, display):
        self.display_pool = display_pool
        self.display = display
        self.jobs = queue.Queue()
        self.ready = threading.Event()
        self.page_ready = threading.Event()
        self.busy = threading.Event()
        self.last_error = None
        self.last_prepare_elapsed = None
        self.last_solve_elapsed = None
        self.completed = 0
        self.failures = 0
        self._browser_ctx = None
        self._browser = None
        self._warm_page = None
        self._thread = threading.Thread(target=self._run, daemon=True)
        self._thread.start()

    def _launch_browser(self):
        self.display_pool.ensure_xvfb(self.display)
        with _display_lock:
            os.environ["DISPLAY"] = f":{self.display}"
            ctx = Camoufox(
                headless=False,
                humanize=True,
                os="linux",
                disable_coop=True,
                window=(1280, 720),
            )
            browser = ctx.__enter__()
        self._browser_ctx = ctx
        self._browser = browser
        self.ready.set()
        self.last_error = None
        print(f"[W{self.display}] Warm browser ready")

    def _close_browser(self):
        self.ready.clear()
        self.page_ready.clear()
        self._close_warm_page()
        if self._browser_ctx:
            try:
                self._browser_ctx.__exit__(None, None, None)
            except Exception:
                pass
        self._browser_ctx = None
        self._browser = None

    def _ensure_browser(self):
        if self._browser is not None:
            return
        try:
            self._launch_browser()
        except Exception as e:
            self.last_error = str(e)
            print(f"[W{self.display}] Warm browser launch failed: {e}")
            time.sleep(2)

    def _close_warm_page(self):
        page = self._warm_page
        self._warm_page = None
        self.page_ready.clear()
        if page:
            try:
                page.close()
            except Exception:
                pass

    def _prepare_warm_page(self):
        if not WARM_PAGE or self._browser is None or self._warm_page is not None:
            return

        started = time.time()
        page = self._browser.new_page()
        try:
            page.add_init_script(INIT_SCRIPT)
            page.route(CHALLENGE_ROUTE, lambda route: route.abort())
            page.goto(default_turnstile_url(), wait_until="domcontentloaded", timeout=15000)
            self._warm_page = page
            self.last_prepare_elapsed = round(time.time() - started, 1)
            self.page_ready.set()
            self.last_error = None
            print(f"[W{self.display}] Warm page ready in {self.last_prepare_elapsed:.1f}s")
        except Exception as e:
            self.last_error = f"Warm page prepare failed: {e}"
            print(f"[W{self.display}] {self.last_error}")
            try:
                page.close()
            except Exception:
                pass
            time.sleep(1)

    def _solve_warm_page(self):
        page = self._warm_page
        self._warm_page = None
        self.page_ready.clear()
        if page is None:
            return _solve_with_browser(self._browser, self.display, start=time.time())

        start = time.time()
        try:
            page.unroute(CHALLENGE_ROUTE)
            page.reload(wait_until="domcontentloaded", timeout=15000)
            return _solve_loaded_page(page, self.display, start=start)
        finally:
            try:
                page.close()
            except Exception:
                pass

    def _run(self):
        while True:
            self._ensure_browser()
            self._prepare_warm_page()
            result_queue, context, done = self.jobs.get()
            self.busy.set()
            if self._browser is None:
                result_queue.put((None, RuntimeError(self.last_error or "Browser not ready")))
                self.busy.clear()
                done.set()
                continue

            started = time.time()
            try:
                if context is None and WARM_PAGE and self.page_ready.is_set():
                    result = self._solve_warm_page()
                else:
                    result = _solve_with_browser(self._browser, self.display, start=time.time(), context=context)
                self.last_solve_elapsed = round(time.time() - started, 1)
                self.completed += 1
                result_queue.put((result, None))
            except Exception as e:
                self.failures += 1
                self.last_error = str(e) if context is None else type(e).__name__
                if context is None:
                    traceback.print_exc()
                else:
                    print(f"[V2] worker error type={type(e).__name__}")
                self._close_browser()
                if context is None and time.time() - started < TIMEOUT_S:
                    try:
                        print(f"[W{self.display}] Retrying once with a fresh warm browser")
                        self._ensure_browser()
                        if self._browser is None:
                            raise RuntimeError(self.last_error or "Browser restart failed")
                        result = _solve_with_browser(self._browser, self.display, start=time.time(), context=context)
                        self.last_solve_elapsed = round(time.time() - started, 1)
                        self.completed += 1
                        result_queue.put((result, None))
                    except Exception as retry_error:
                        self.failures += 1
                        self.last_error = str(retry_error)
                        traceback.print_exc()
                        self._close_browser()
                        result_queue.put((None, retry_error))
                else:
                    result_queue.put((None, e))
            finally:
                self.busy.clear()
                done.set()

    def generate(self, timeout, context=None, done=None):
        done = done if done is not None else threading.Event()
        result_queue = queue.Queue(maxsize=1)
        self.jobs.put((result_queue, context, done))
        try:
            result, error = result_queue.get(timeout=timeout)
        except queue.Empty:
            return {"ok": False, "error": "Warm browser worker timed out"}
        if error:
            raise error
        return result


class WarmBrowserPool:
    def __init__(self, size):
        self.size = size
        self.display_pool = WorkerPool(size)
        self.queue = queue.Queue()
        self.workers = []

        for display in range(100, 100 + size):
            worker = WarmBrowserWorker(self.display_pool, display)
            self.workers.append(worker)
            self.queue.put(worker)

    def acquire(self, timeout=60):
        try:
            worker = self.queue.get(timeout=timeout)
        except queue.Empty:
            return None

        if not worker.ready.wait(timeout=min(30, timeout)):
            self.queue.put(worker)
            return None
        return worker

    def release(self, worker):
        self.queue.put(worker)

    @property
    def available(self):
        return self.queue.qsize()

    @property
    def ready_pages(self):
        if not WARM_PAGE:
            return 0
        return sum(1 for worker in self.workers if worker.page_ready.is_set())

    @property
    def busy(self):
        return sum(1 for worker in self.workers if worker.busy.is_set())

    def health(self):
        ok, displays = self.display_pool.health()
        for worker in self.workers:
            worker_state = displays[f":{worker.display}"]
            worker_state["browser_ready"] = worker.ready.is_set()
            worker_state["page_ready"] = worker.page_ready.is_set()
            worker_state["busy"] = worker.busy.is_set()
            worker_state["browser_error"] = worker.last_error
            worker_state["last_prepare_elapsed"] = worker.last_prepare_elapsed
            worker_state["last_solve_elapsed"] = worker.last_solve_elapsed
            worker_state["completed"] = worker.completed
            worker_state["failures"] = worker.failures
            ok = ok and worker.ready.is_set()
        return ok, displays


# --- Flask endpoints ---

@app.after_request
def prevent_token_caching(response):
    if request.path == "/v2/token":
        response.headers["Cache-Control"] = "no-store"
    return response


@app.post("/v2/token")
def context_token_endpoint():
    if request.content_length is not None and request.content_length > 8192:
        return jsonify(ok=False, code="invalid_request", error="Request too large"), 413
    try:
        context = parse_context(request.get_json(silent=True))
    except ConfigError as exc:
        return jsonify(ok=False, code="not_configured", error=str(exc)), 503
    except ValueError as exc:
        return jsonify(ok=False, code="invalid_request", error=str(exc)), 400
    if pool is None:
        return jsonify(ok=False, code="unavailable"), 503
    started = time.monotonic()
    worker = pool.acquire(timeout=min(POOL_ACQUIRE_TIMEOUT_S, 0.05))
    if worker is None:
        return jsonify(ok=False, code="busy", request_id=context.request_id), 503
    acquired = time.monotonic()
    done = threading.Event()
    print(f"[V2] begin request={context.request_id} context={context.context_sha256[:12]}")
    try:
        if hasattr(worker, "generate"):
            result = worker.generate(timeout=CONTEXT_REQUEST_TIMEOUT_S, context=context, done=done)
        else:
            result = get_token_on_display(worker, context=context, done=done)
        result = {**result, "api_version": 2, "request_id": context.request_id,
                  "context_sha256": context.context_sha256,
                  "queue_elapsed": round(acquired - started, 3),
                  "total_elapsed": round(time.monotonic() - started, 3)}
        status = 200 if result.get("ok") else (422 if "reason" in result or result.get("code") == "telemetry_unconfirmed" else 504)
        print(f"[V2] end request={context.request_id} status={status} total={result['total_elapsed']}")
        return jsonify(result), status
    except Exception as exc:
        # Browser exceptions may include navigation URLs; do not log or return raw context.
        print(f"[V2] error request={context.request_id} type={type(exc).__name__}")
        return jsonify(ok=False, code="worker_error", request_id=context.request_id), 500
    finally:
        release_when_done(done, lambda: pool.release(worker))


@app.route("/health")
def health():
    xvfb_ok, displays = pool.health()
    status_code = 200 if xvfb_ok else 503
    return jsonify({
        "status": "ok" if xvfb_ok else "degraded",
        "mode": "warm-page" if WARM_PAGE and REUSE_BROWSER else ("warm-browser" if REUSE_BROWSER else "cold-browser"),
        "api_version": 2,
        "context_tokens": True,
        "workers": pool.size,
        "available": pool.available,
        "busy": getattr(pool, "busy", None),
        "ready_pages": getattr(pool, "ready_pages", None),
        "displays": displays,
    }), status_code


if __name__ == "__main__":
    try:
        allowed_host()
    except ConfigError as exc:
        raise SystemExit(f"error: {exc}")
    pool = WarmBrowserPool(WORKERS) if REUSE_BROWSER else WorkerPool(WORKERS)
    print(f"\nTurnstile token service on http://localhost:{PORT}")
    print(f"  Workers: {WORKERS} (concurrent requests)")
    print(f"  Pool acquire timeout: {POOL_ACQUIRE_TIMEOUT_S}s")
    print(f"  Browser reuse: {REUSE_BROWSER}")
    print(f"  Warm page: {WARM_PAGE}")
    print(f"  POST /v2/token → solve Turnstile for a verification context")
    print(f"  GET /health  → healthcheck + worker status")

    from waitress import serve
    serve(app, host="0.0.0.0", port=PORT, threads=WORKERS + 2)
