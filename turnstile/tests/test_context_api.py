import concurrent.futures
from contextlib import redirect_stdout
import io
import queue
import threading
import unittest
from types import SimpleNamespace
from unittest.mock import patch

import os
import sys
try:
    import camoufox.sync_api  # noqa: F401
except ImportError:
    from types import ModuleType
    _pkg, _sub = ModuleType('camoufox'), ModuleType('camoufox.sync_api')
    _sub.Camoufox = object
    _pkg.sync_api = _sub
    sys.modules['camoufox'], sys.modules['camoufox.sync_api'] = _pkg, _sub

os.environ['API_DOMAIN'] = 'app.example.com'

import server
from context_api import ConfigError, allowed_host, parse_context, TelemetryReceipt, release_when_done, utc_now

URL = 'https://app.example.com/turnstile?v=5#sid=' + 'a' * 32 + '&cd=Context_123&tc=Payload.Signature'


class FakeWorker:
    def __init__(self, display=100):
        self.display = display
        self.contexts = []

    def generate(self, timeout, context=None, done=None):
        self.contexts.append(context)
        done.set()
        return {'ok': True, 'token': 'synthetic-test-token-never-sent-anywhere',
                'elapsed': 0, 'token_observed_at': utc_now(), 'telemetry_acknowledged': True}


class FakePool:
    size = 12

    def __init__(self):
        self.queue = queue.Queue()
        self.workers = [FakeWorker(100 + i) for i in range(self.size)]
        for w in self.workers:
            self.queue.put(w)

    @property
    def available(self):
        return self.queue.qsize()

    def acquire(self, timeout):
        try:
            return self.queue.get(timeout=timeout)
        except queue.Empty:
            return None

    def release(self, worker):
        self.queue.put(worker)


class ContextTests(unittest.TestCase):
    def test_allowed_host_comes_from_env(self):
        with patch.dict(os.environ, {'API_DOMAIN': 'Svc.Example.org'}):
            self.assertEqual(allowed_host(), 'svc.example.org')
            other = URL.replace('app.example.com', 'svc.example.org')
            self.assertEqual(parse_context({'turnstileUrl': other, 'requestId': 'test:1'}).url, other)
            with self.assertRaises(ValueError):
                parse_context({'turnstileUrl': URL, 'requestId': 'test:1'})

    def test_missing_or_bad_api_domain_is_a_config_error(self):
        for value in [None, '', 'https://app.example.com', 'app.example.com/x']:
            env = {k: v for k, v in os.environ.items() if k != 'API_DOMAIN'}
            if value is not None:
                env['API_DOMAIN'] = value
            with self.subTest(value=value), patch.dict(os.environ, env, clear=True):
                with self.assertRaises(ConfigError) as cm:
                    parse_context({'turnstileUrl': URL, 'requestId': 'test:1'})
                self.assertIn('API_DOMAIN', str(cm.exception))

    def test_window_title_match_defaults_to_first_label(self):
        with patch.dict(os.environ, {'API_DOMAIN': 'app.example.com'}):
            os.environ.pop('WINDOW_TITLE_MATCH', None)
            self.assertEqual(server.window_title_match(), 'app')
            os.environ['WINDOW_TITLE_MATCH'] = 'Custom'
            self.assertEqual(server.window_title_match(), 'Custom')
            self.assertEqual(server.default_turnstile_url(), 'https://app.example.com/turnstile?v=1')

    def test_valid_context_preserves_fragment(self):
        context = parse_context({'turnstileUrl': URL, 'requestId': '638203:1'})
        self.assertEqual(context.url, URL)
        self.assertEqual(len(context.context_sha256), 64)

    def test_rejects_wrong_hosts_paths_duplicate_or_missing_context(self):
        urls = [URL.replace('https:', 'http:'), URL.replace('app.example.com', 'evil.example'),
                URL.replace('app.example.com', 'app.example.com.evil.example'),
                URL.replace('app.example.com', 'evil@app.example.com'),
                URL.replace('app.example.com', 'app.example.com:443'),
                URL.replace('/turnstile?', '/anything?'), URL.split('#')[0],
                URL + '&sid=' + 'a' * 32, URL.replace('v=5', 'v=5&v=5'),
                URL.replace('Context_123', 'bad%0Acontext'), URL + '\n',
                URL.replace('tc=Payload.Signature', 'tc='), URL + '&other=x']
        for url in urls:
            with self.subTest(url=url), self.assertRaises(ValueError):
                parse_context({'turnstileUrl': url, 'requestId': 'test:1'})

    def test_request_id_and_body_shape(self):
        for value in [None, [], {}, {'turnstileUrl': URL},
                      {'turnstileUrl': URL, 'requestId': 'new\nline'},
                      {'turnstileUrl': URL, 'requestId': 'ok', 'extra': True}]:
            with self.subTest(value=value), self.assertRaises(ValueError):
                parse_context(value)

    def test_telemetry_only_matches_real_success_for_same_session(self):
        context = parse_context({'turnstileUrl': URL, 'requestId': 'test:1'})
        receipt = TelemetryReceipt(context)
        def response(event='solve_success', sid='a' * 32, status=204):
            return SimpleNamespace(url='https://app.example.com/turnstile/telemetry', status=status,
                                   request=SimpleNamespace(method='POST', post_data_json={
                                       'eventType': event, 'sessionId': sid}))
        for r in [response('page_loaded'), response(sid='b' * 32), response(status=500)]:
            receipt.response(r)
            self.assertFalse(receipt.success)
        receipt.response(response())
        self.assertTrue(receipt.success)

    def test_unacknowledged_telemetry_does_not_return_token(self):
        receipt = TelemetryReceipt(parse_context({'turnstileUrl': URL, 'requestId': 'test:1'}))
        result = receipt.finish(None, {'ok': True, 'token': 'not-returned'}, timeout_s=0)
        self.assertFalse(result['ok'])
        self.assertNotIn('token', result)

    def test_release_is_deferred_until_real_completion(self):
        done, released = threading.Event(), threading.Event()
        release_when_done(done, released.set)
        self.assertFalse(released.wait(0.02))
        done.set()
        self.assertTrue(released.wait(1))

    def test_page_is_closed_after_telemetry_receipt(self):
        context = parse_context({'turnstileUrl': URL, 'requestId': 'test:1'})
        calls = []
        class Page:
            def add_init_script(self, script): pass
            def on(self, event, callback): self.callback = callback
            def goto(self, url, **kwargs): self.url = url
            def wait_for_timeout(self, ms):
                self.callback(SimpleNamespace(url='https://app.example.com/turnstile/telemetry', status=204,
                    request=SimpleNamespace(method='POST', post_data_json={'sessionId': 'a' * 32, 'eventType': 'solve_success'})))
                calls.append('ack')
            def close(self): calls.append('close')
        page = Page()
        with patch.object(server, '_solve_loaded_page', return_value={'ok': True, 'token': 'synthetic'}):
            result = server._solve_with_browser(SimpleNamespace(new_page=lambda: page), 100, context=context)
        self.assertEqual(page.url, URL)
        self.assertTrue(result['telemetry_acknowledged'])
        self.assertEqual(calls, ['ack', 'close'])


class EndpointTests(unittest.TestCase):
    def setUp(self):
        self.pool = FakePool()
        self.patch = patch.object(server, 'pool', self.pool)
        self.patch.start()
        self.addCleanup(self.patch.stop)
        self.client = server.app.test_client()

    def test_token_endpoint_requires_api_domain(self):
        env = {k: v for k, v in os.environ.items() if k != 'API_DOMAIN'}
        with patch.dict(os.environ, env, clear=True):
            r = self.client.post('/v2/token', json={'turnstileUrl': URL, 'requestId': 'exec:1'})
        self.assertEqual(r.status_code, 503)
        self.assertEqual(r.json['code'], 'not_configured')
        self.assertIn('API_DOMAIN', r.json['error'])

    def test_new_endpoint_correlates_request_without_logging_secrets(self):
        output = io.StringIO()
        with redirect_stdout(output):
            r = self.client.post('/v2/token', json={'turnstileUrl': URL, 'requestId': 'exec:1'})
        self.assertEqual(r.status_code, 200)
        self.assertEqual(r.json['request_id'], 'exec:1')
        self.assertEqual(r.json['api_version'], 2)
        self.assertEqual(r.headers['Cache-Control'], 'no-store')
        self.assertNotIn(URL, output.getvalue())
        self.assertNotIn(r.json['token'], output.getvalue())
        self.assertEqual(self.pool.available, 12)

    def test_bad_context_never_takes_worker(self):
        self.assertEqual(self.client.post('/v2/token', json={'turnstileUrl': 'http://localhost', 'requestId': 'exec:1'}).status_code, 400)
        self.assertEqual(self.pool.available, 12)

    def test_busy_is_safe_to_retry(self):
        with patch.object(self.pool, 'acquire', return_value=None):
            r = self.client.post('/v2/token', json={'turnstileUrl': URL, 'requestId': 'exec:1'})
        self.assertEqual(r.status_code, 503)
        self.assertEqual(r.json['code'], 'busy')

    def test_legacy_token_route_removed(self):
        self.assertEqual(self.client.get('/token').status_code, 404)
        self.assertEqual(self.pool.workers[0].contexts, [])

    def test_v2_token_route_kept(self):
        r = self.client.post('/v2/token', json={'turnstileUrl': URL, 'requestId': 'exec:1'})
        self.assertEqual(r.status_code, 200)
        self.assertEqual(r.headers['Cache-Control'], 'no-store')

    def test_http_timeout_does_not_reissue_busy_worker(self):
        done_box = []
        worker = FakeWorker()
        def timeout(**kwargs):
            done_box.append(kwargs['done'])
            return {'ok': False, 'error': 'Worker timed out'}
        released = threading.Event()
        with patch.object(self.pool, 'acquire', return_value=worker), \
             patch.object(worker, 'generate', side_effect=timeout), \
             patch.object(self.pool, 'release', side_effect=lambda w: released.set()):
            r = self.client.post('/v2/token', json={'turnstileUrl': URL, 'requestId': 'exec:1'})
            self.assertEqual(r.status_code, 504)
            self.assertFalse(released.wait(0.02))
            done_box[0].set()
            self.assertTrue(released.wait(1))

    def test_twelve_concurrent_requests_keep_their_context(self):
        barrier = threading.Barrier(12)
        original_generate = FakeWorker.generate
        def synchronized_generate(worker, **kwargs):
            barrier.wait(timeout=3)
            return original_generate(worker, **kwargs)
        def one(i):
            url = URL.replace('a' * 32, f'{i:032x}')
            with server.app.test_client() as client:
                response = client.post('/v2/token', json={'turnstileUrl': url, 'requestId': f'exec:{i}'})
                self.assertEqual(response.status_code, 200)
                self.assertEqual(response.json['context_sha256'], parse_context({'turnstileUrl': url, 'requestId': f'exec:{i}'}).context_sha256)
        with patch.object(FakeWorker, 'generate', synchronized_generate), \
             concurrent.futures.ThreadPoolExecutor(max_workers=12) as executor:
            list(executor.map(one, range(12)))
        self.assertEqual(self.pool.available, 12)

    def test_actual_warm_worker_passes_context_and_completes_lease(self):
        def ready(worker):
            worker._browser = object()
            worker.ready.set()
        context = parse_context({'turnstileUrl': URL, 'requestId': 'exec:job'})
        done = threading.Event()
        def solve(browser, display, start=None, context=None):
            self.assertEqual(context.url, URL)
            return {'ok': True, 'token': 'synthetic'}
        with patch.object(server.WarmBrowserWorker, '_ensure_browser', ready), \
             patch.object(server.WarmBrowserWorker, '_prepare_warm_page'), \
             patch.object(server, '_solve_with_browser', side_effect=solve):
            worker = server.WarmBrowserWorker(None, 100)
            result = worker.generate(timeout=1, context=context, done=done)
            self.assertTrue(result['ok'])
            self.assertTrue(done.wait(1))


if __name__ == '__main__':
    unittest.main()
