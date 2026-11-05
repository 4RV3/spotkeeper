"""Request-scoped verification context; never fabricates challenge telemetry."""
from dataclasses import dataclass
from datetime import datetime, timezone
import hashlib
import os
import re
import threading
import time
from urllib.parse import parse_qs, urlsplit


class ConfigError(Exception):
    """The service is not configured (API_DOMAIN missing or invalid)."""


_HOST_RE = re.compile(r"[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)*")


def allowed_host():
    """The only host whose verification URLs are accepted: env API_DOMAIN (host only)."""
    host = os.environ.get("API_DOMAIN", "").strip().lower()
    if not host:
        raise ConfigError("API_DOMAIN is not set: put the reservation service host (e.g. app.example.com) in the API_DOMAIN environment variable")
    if not _HOST_RE.fullmatch(host):
        raise ConfigError("API_DOMAIN must be a host name only, without https:// or a path")
    return host


def utc_now():
    return datetime.now(timezone.utc).isoformat()


@dataclass(frozen=True)
class VerificationContext:
    url: str
    request_id: str
    session_id: str
    context_sha256: str


def parse_context(payload):
    if not isinstance(payload, dict) or set(payload) - {"turnstileUrl", "requestId"}:
        raise ValueError("Expected turnstileUrl and requestId")
    url = payload.get("turnstileUrl")
    if not isinstance(url, str) or len(url) > 4096 or any(ord(c) <= 32 for c in url) or "\\" in url:
        raise ValueError("Invalid turnstileUrl")
    host = allowed_host()
    try:
        parsed = urlsplit(url)
        if (parsed.scheme != "https" or parsed.netloc != host
                or parsed.path != "/turnstile"):
            raise ValueError("Only the configured API_DOMAIN host and /turnstile path are accepted")
        query = parse_qs(parsed.query, keep_blank_values=True, strict_parsing=True)
        fragment = parse_qs(parsed.fragment, keep_blank_values=True, strict_parsing=True)
    except ValueError:
        raise ValueError("Invalid verification URL") from None
    if set(query) != {"v"} or len(query["v"]) != 1 or not re.fullmatch(r"[1-9][0-9]{0,2}", query["v"][0]):
        raise ValueError("Expected a verification URL revision")
    if set(fragment) != {"sid", "cd", "tc"} or any(len(v) != 1 for v in fragment.values()):
        raise ValueError("Expected unique sid, cd and tc parameters")
    sid, cd, tc = (fragment[k][0] for k in ("sid", "cd", "tc"))
    if not re.fullmatch(r"[a-fA-F0-9]{32}", sid):
        raise ValueError("Invalid session identifier")
    if not re.fullmatch(r"[A-Za-z0-9_-]{1,255}", cd):
        raise ValueError("Invalid challenge context")
    if not re.fullmatch(r"[A-Za-z0-9_-]{1,2048}\.[A-Za-z0-9_-]{1,256}", tc):
        raise ValueError("Invalid telemetry context")
    request_id = payload.get("requestId", "")
    if not isinstance(request_id, str) or not re.fullmatch(r"[A-Za-z0-9_.:-]{1,128}", request_id):
        raise ValueError("A nonempty requestId is required")
    return VerificationContext(url, request_id, sid, hashlib.sha256(url.encode()).hexdigest())


class TelemetryReceipt:
    """Observe the real page's response, without sending or changing telemetry."""
    def __init__(self, context):
        self.context = context
        self.host = urlsplit(context.url).netloc  # already validated against API_DOMAIN
        self.success = False

    def response(self, response):
        try:
            parsed = urlsplit(response.url)
            if (parsed.scheme != "https" or parsed.netloc != self.host
                    or parsed.path != "/turnstile/telemetry"
                    or response.request.method != "POST" or not 200 <= response.status < 300):
                return
            data = response.request.post_data_json
            if (isinstance(data, dict) and data.get("sessionId") == self.context.session_id
                    and data.get("eventType") == "solve_success"):
                self.success = True
        except Exception:
            # Malformed/unrelated responses cannot make a context valid.
            pass

    def finish(self, page, result, timeout_s=5):
        if not result.get("ok"):
            return result
        observed_at = utc_now()
        deadline = time.monotonic() + timeout_s
        while not self.success and time.monotonic() < deadline:
            page.wait_for_timeout(50)
        if not self.success:
            return {"ok": False, "code": "telemetry_unconfirmed",
                    "error": "The page's solve-success telemetry was not acknowledged"}
        return {**result, "token_observed_at": observed_at, "telemetry_acknowledged": True}


def release_when_done(done, release):
    """A timed-out HTTP request must not free a still-running browser slot."""
    if done.is_set():
        release()
    else:
        def wait_and_release():
            done.wait()
            release()
        threading.Thread(target=wait_and_release, daemon=True).start()
