from http.server import ThreadingHTTPServer, BaseHTTPRequestHandler
import requests, os, json, time, pathlib, sys, logging, signal, uuid, threading
from requests.exceptions import ChunkedEncodingError as _ChunkedEncError
from logging.handlers import RotatingFileHandler
from requests.adapters import HTTPAdapter
from urllib3.util.retry import Retry

LOG = pathlib.Path(os.environ.get(
    'OPENCODE_PROXY_LOG',
    str(pathlib.Path(__file__).resolve().parent / 'opencode-proxy.log')
))
# PID file path — helps process management tools (supervisord, Docker HEALTHCHECK
# scripts, Windows service wrappers) discover and signal the proxy.
PID_FILE = pathlib.Path(os.environ.get(
    'OPENCODE_PROXY_PID_FILE',
    str(pathlib.Path(__file__).resolve().parent / 'opencode-proxy.pid')
))
TARGET_ORIGIN = os.environ.get(
    'OPENCODE_PROXY_TARGET',
    'https://opencode.ai'
)
LOG_LEVEL = os.environ.get(
    'OPENCODE_PROXY_LOG_LEVEL',
    'INFO'
).upper()
MAX_BODY_BYTES = int(os.environ.get(
    'OPENCODE_PROXY_MAX_BODY_BYTES',
    str(100 * 1024 * 1024),  # 100 MB default
))
# CORS: allowed origin. '*' allows any origin; set to a specific value in production.
CORS_ORIGIN = os.environ.get(
    'OPENCODE_PROXY_CORS_ORIGIN',
    '*'
)
# Upstream request timeouts (seconds): connect timeout and read timeout.
CONNECT_TIMEOUT = int(os.environ.get('OPENCODE_PROXY_CONNECT_TIMEOUT', '10'))
READ_TIMEOUT = int(os.environ.get('OPENCODE_PROXY_READ_TIMEOUT', '300'))
# Interval (seconds) for periodic stats auto-save to prevent data loss on crash.
STATS_SAVE_INTERVAL = int(os.environ.get('OPENCODE_PROXY_STATS_SAVE_INTERVAL', '60'))
# Upstream retry: max attempts (including the initial one).
# When > 1, transient failures (429, 502–504) are retried with exponential backoff.
UPSTREAM_MAX_RETRIES = int(os.environ.get('OPENCODE_PROXY_UPSTREAM_MAX_RETRIES', '2'))
# Exponential backoff base (seconds): actual wait = backoff_base * 2^attempt.
UPSTREAM_RETRY_BACKOFF_BASE = float(
    os.environ.get('OPENCODE_PROXY_UPSTREAM_RETRY_BACKOFF', '1.0')
)

_LEVEL_MAP = {
    'DEBUG': logging.DEBUG,
    'INFO': logging.INFO,
    'WARNING': logging.WARNING,
    'ERROR': logging.ERROR,
}

# ---------------------------------------------------------------------------
# Persistent HTTP session with connection pooling for upstream requests
# ---------------------------------------------------------------------------
_upstream_session = requests.Session()
# Pool: 10 connections per host, keep alive 60s
_adapter = HTTPAdapter(
    pool_connections=10,
    pool_maxsize=20,
    max_retries=Retry(
        total=0,  # retries handled at proxy level
        connect=0,
        read=0,
        redirect=0,
        status=0,
        other=0,
        backoff_factor=0,
    ),
)
_upstream_session.mount('http://', _adapter)
_upstream_session.mount('https://', _adapter)
_upstream_session.headers.update({
    'User-Agent': 'AI-Model-Gateway/1.4 opencode-proxy',
    'Connection': 'keep-alive',
})

# ---------------------------------------------------------------------------
# Logger with rotation: 10 MB per file, keep 5 backups
# ---------------------------------------------------------------------------
logger = logging.getLogger('opencode-proxy')
logger.setLevel(_LEVEL_MAP.get(LOG_LEVEL, logging.INFO))
_rfh = RotatingFileHandler(
    str(LOG), maxBytes=10 * 1024 * 1024, backupCount=5, encoding='utf-8'
)
_rfh.setFormatter(logging.Formatter('%(asctime)s %(levelname)s %(message)s'))
logger.addHandler(_rfh)

# Also log to stderr so the console isn't silent
_stderr_h = logging.StreamHandler(sys.stderr)
_stderr_h.setFormatter(logging.Formatter('%(asctime)s %(levelname)s %(message)s'))
logger.addHandler(_stderr_h)

# ---------------------------------------------------------------------------
# Paths that should NOT be proxied (health / internal)
# ---------------------------------------------------------------------------
HEALTH_PATHS = frozenset({'/health', '/healthz', '/-/health'})
# Admin / monitoring paths served by the proxy (not forwarded upstream)
STATS_PATHS = frozenset({'/stats', '/-/stats'})
METRICS_PATHS = frozenset({'/metrics', '/-/metrics', '/prometheus'})
BLACKLIST_HEADERS = frozenset({
    'host', 'content-length', 'connection', 'accept-encoding',
})
# HTTP status codes considered retryable for transient upstream failures
_RETRYABLE_STATUSES = frozenset({429, 500, 502, 503, 504})

# Streaming detection — only text/event-stream is truly streaming by content-type.
# text/plain is NOT included: non-streaming endpoints (error messages, plain text
# responses) commonly use it. When a provider sends SSE over text/plain and the
# client requested stream=true, the is_streaming flag covers that case.
_STREAM_CT_PREFIXES = ('text/event-stream',)

# ---------------------------------------------------------------------------
# Stats persistence path — same directory as the log file, JSON format
# ---------------------------------------------------------------------------
STATS_FILE = LOG.with_suffix('.stats.json')

# ---------------------------------------------------------------------------
# Accumulated proxy statistics (thread-safe)
# ---------------------------------------------------------------------------
_stats_lock = threading.Lock()

_stats = {
    'started_at': time.time(),
    'total_requests': 0,
    'total_errors': 0,
    'total_bytes_sent': 0,
    'total_latency_ms': 0,
    'total_upstream_retries': 0,
    'total_tokens_in': 0,
    'total_tokens_out': 0,
    'total_tokens_reasoning': 0,
    'total_tokens_total': 0,
    'status_codes': {},   # {200: N, 502: N, ...}
    'errors': {},         # {'timeout': N, 'connection_error': N, ...}
    'per_model': {},      # {'model-name': {requests, errors, tokens_in, tokens_out, tokens_total, avg_latency_ms, bytes_sent}}
}

def _load_stats():
    """Merge saved stats from disk so cumulative totals survive restart."""
    global _stats
    if not STATS_FILE.exists():
        return
    try:
        saved = json.loads(STATS_FILE.read_text('utf-8'))
        with _stats_lock:
            # Sum merge-only fields; 'started_at' stays the current boot time.
            _stats['total_requests'] += saved.get('total_requests', 0)
            _stats['total_errors'] += saved.get('total_errors', 0)
            _stats['total_bytes_sent'] += saved.get('total_bytes_sent', 0)
            _stats['total_latency_ms'] += saved.get('total_latency_ms', 0)
            _stats['total_upstream_retries'] += saved.get('total_upstream_retries', 0)
            _stats['total_tokens_in'] += saved.get('total_tokens_in', 0)
            _stats['total_tokens_out'] += saved.get('total_tokens_out', 0)
            _stats['total_tokens_reasoning'] += saved.get('total_tokens_reasoning', 0)
            _stats['total_tokens_total'] += saved.get('total_tokens_total', 0)
            for sc, n in saved.get('status_codes', {}).items():
                _stats['status_codes'][sc] = _stats['status_codes'].get(sc, 0) + n
            for err, n in saved.get('errors', {}).items():
                _stats['errors'][err] = _stats['errors'].get(err, 0) + n
            for model_name, mdata in saved.get('per_model', {}).items():
                m = _stats['per_model'].setdefault(model_name, {
                    'requests': 0, 'errors': 0, 'tokens_in': 0, 'tokens_out': 0,
                    'tokens_reasoning': 0, 'tokens_total': 0,
                    'total_latency_ms': 0, 'bytes_sent': 0,
                })
                m['requests'] += mdata.get('requests', 0)
                m['errors'] += mdata.get('errors', 0)
                m['tokens_in'] += mdata.get('tokens_in', 0)
                m['tokens_out'] += mdata.get('tokens_out', 0)
                m['tokens_reasoning'] += mdata.get('tokens_reasoning', 0)
                m['tokens_total'] += mdata.get('tokens_total', 0)
                m['total_latency_ms'] += mdata.get('total_latency_ms', 0)
                m['bytes_sent'] += mdata.get('bytes_sent', 0)
        logger.info('stats restored from %s (%d prior requests)',
                    STATS_FILE.name, saved.get('total_requests', 0))
    except Exception as exc:
        logger.warning('could not load saved stats from %s: %s', STATS_FILE.name, exc)


def _save_stats():
    """Persist current stats to disk so they survive restart."""
    try:
        with _stats_lock:
            snapshot = dict(_stats)
        # started_at is per-run; write a sentinel so load merges correctly
        snapshot.pop('started_at', None)
        STATS_FILE.parent.mkdir(parents=True, exist_ok=True)
        STATS_FILE.write_text(json.dumps(snapshot, indent=2, ensure_ascii=False), 'utf-8')
    except Exception as exc:
        logger.warning('could not save stats to %s: %s', STATS_FILE.name, exc)

# ---------------------------------------------------------------------------
# Periodic auto-save: background thread that persists stats at a configurable
# interval so cumulative totals survive a hard crash (SIGKILL, OOM, power loss)
# where the graceful shutdown handler never runs.
# ---------------------------------------------------------------------------
_stats_saver_stop = threading.Event()


def _write_pid_file():
    """Write current PID to file so process management tools can find us."""
    try:
        PID_FILE.write_text(str(os.getpid()), 'utf-8')
        logger.info('pid file written: %s (pid=%d)', PID_FILE, os.getpid())
    except Exception as exc:
        logger.warning('could not write pid file %s: %s', PID_FILE, exc)


def _remove_pid_file():
    """Remove PID file on shutdown."""
    try:
        if PID_FILE.exists():
            PID_FILE.unlink()
            logger.info('pid file removed: %s', PID_FILE)
    except Exception as exc:
        logger.warning('could not remove pid file %s: %s', PID_FILE, exc)


def _stats_auto_save_loop():
    """Daemon thread: wake every STATS_SAVE_INTERVAL seconds and persist stats."""
    while not _stats_saver_stop.wait(STATS_SAVE_INTERVAL):
        _save_stats()
    # Final save on thread exit
    _save_stats()

# ---------------------------------------------------------------------------
# Active request tracking for graceful drain on shutdown
# ---------------------------------------------------------------------------
_active_requests = set()
_active_requests_lock = threading.Lock()


class GracefulHTTPServer(ThreadingHTTPServer):
    """ThreadingHTTPServer that tracks in-flight request threads so the
    shutdown handler can drain them before closing the upstream session."""

    allow_reuse_address = True

    def process_request(self, request, client_address):
        t = threading.Thread(
            target=self._handle_with_tracking,
            args=(request, client_address),
        )
        t.daemon = True
        t.start()

    def _handle_with_tracking(self, request, client_address):
        t = threading.current_thread()
        with _active_requests_lock:
            _active_requests.add(t)
        try:
            self.finish_request(request, client_address)
        except Exception:
            self.handle_error(request, client_address)
        finally:
            try:
                self.shutdown_request(request)
            except Exception:
                pass
            with _active_requests_lock:
                _active_requests.discard(t)


def _record_stats(status_code, bytes_sent, elapsed_ms, error_type=None, retries=0,
                  model=None, tokens_in=0, tokens_out=0, tokens_reasoning=0):
    with _stats_lock:
        _stats['total_requests'] += 1
        _stats['total_bytes_sent'] += bytes_sent
        _stats['total_latency_ms'] += elapsed_ms
        _stats['total_upstream_retries'] += retries
        _stats['total_tokens_in'] += tokens_in
        _stats['total_tokens_out'] += tokens_out
        _stats['total_tokens_reasoning'] += tokens_reasoning
        _stats['total_tokens_total'] += tokens_in + tokens_out + tokens_reasoning
        sc = str(status_code)
        _stats['status_codes'][sc] = _stats['status_codes'].get(sc, 0) + 1
        if error_type:
            _stats['total_errors'] += 1
            _stats['errors'][error_type] = _stats['errors'].get(error_type, 0) + 1
        if model:
            m = _stats['per_model'].setdefault(model, {
                'requests': 0, 'errors': 0, 'tokens_in': 0, 'tokens_out': 0,
                'tokens_reasoning': 0, 'tokens_total': 0,
                'total_latency_ms': 0, 'bytes_sent': 0,
            })
            m['requests'] += 1
            m['bytes_sent'] += bytes_sent
            m['total_latency_ms'] += elapsed_ms
            m['tokens_in'] += tokens_in
            m['tokens_out'] += tokens_out
            m['tokens_reasoning'] += tokens_reasoning
            m['tokens_total'] += tokens_in + tokens_out + tokens_reasoning
            if error_type:
                m['errors'] += 1


class H(BaseHTTPRequestHandler):
    protocol_version = 'HTTP/1.1'

    def log_message(self, fmt, *args):
        # Suppress default http.server logging — we use our own logger
        return

    def _send_cors_headers(self, extra_origin=None):
        """Attach standard CORS headers so browser-based clients can use the proxy."""
        origin = extra_origin or CORS_ORIGIN
        self.send_header('Access-Control-Allow-Origin', origin)
        if origin != '*':
            self.send_header('Vary', 'Origin')
        self.send_header('Access-Control-Allow-Methods',
                         'GET, POST, PUT, DELETE, PATCH, OPTIONS, HEAD')
        self.send_header('Access-Control-Allow-Headers',
                         'Authorization, Content-Type, X-Api-Key, X-Request-Id')
        self.send_header('Access-Control-Max-Age', '86400')
        self.send_header('Access-Control-Expose-Headers', 'X-Request-Id')

    # ---- Health check with stats ----
    def _health(self):
        with _stats_lock:
            total = _stats['total_requests']
            total_errors = _stats['total_errors']
            total_bytes = _stats['total_bytes_sent']
            total_retries = _stats['total_upstream_retries']
            avg_lat = (
                round(_stats['total_latency_ms'] / total) if total > 0 else 0
            )
            uptime = round(time.time() - _stats['started_at'])
            tokens_total = _stats['total_tokens_total']
            model_count = len(_stats['per_model'])
            status_codes = dict(_stats['status_codes'])
            errors = dict(_stats['errors'])
        with _active_requests_lock:
            active = len(_active_requests)
        body = json.dumps({
            'status': 'ok',
            'proxy': 'opencode',
            'target': TARGET_ORIGIN,
            'uptime_seconds': uptime,
            'active_requests': active,
            'stats': {
                'total_requests': total,
                'total_errors': total_errors,
                'total_bytes_sent': total_bytes,
                'total_upstream_retries': total_retries,
                'avg_latency_ms': avg_lat,
                'total_tokens': tokens_total,
                'models_tracked': model_count,
                'status_codes': status_codes,
                'errors': errors,
                'persisted': STATS_FILE.exists(),
            },
            'ts': time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime()),
        }).encode()
        self.send_response(200)
        self.send_header('Content-Type', 'application/json')
        self.send_header('Content-Length', str(len(body)))
        self._send_cors_headers()
        self.end_headers()
        self.wfile.write(body)

    # ---- Full stats endpoint for operational monitoring ----
    def _stats_endpoint(self):
        """Return full proxy statistics as JSON for operational dashboards."""
        with _stats_lock:
            total_req = _stats['total_requests']
            # Build per-model summary with computed averages
            per_model = {}
            for model_name, mdata in _stats['per_model'].items():
                m = dict(mdata)
                m['avg_latency_ms'] = (
                    round(mdata['total_latency_ms'] / mdata['requests'])
                    if mdata['requests'] > 0 else 0
                )
                per_model[model_name] = m
            snapshot = {
                'started_at': _stats['started_at'],
                'uptime_seconds': round(time.time() - _stats['started_at']),
                'total_requests': total_req,
                'total_errors': _stats['total_errors'],
                'total_bytes_sent': _stats['total_bytes_sent'],
                'total_latency_ms': _stats['total_latency_ms'],
                'total_upstream_retries': _stats['total_upstream_retries'],
                'total_tokens_in': _stats['total_tokens_in'],
                'total_tokens_out': _stats['total_tokens_out'],
                'total_tokens_reasoning': _stats['total_tokens_reasoning'],
                'total_tokens_total': _stats['total_tokens_total'],
                'avg_latency_ms': (
                    round(_stats['total_latency_ms'] / total_req)
                    if total_req > 0 else 0
                ),
                'status_codes': dict(_stats['status_codes']),
                'errors': dict(_stats['errors']),
                'per_model': per_model,
                'persisted': STATS_FILE.exists(),
            }
        with _active_requests_lock:
            active = len(_active_requests)
        snapshot['active_requests'] = active
        body = json.dumps({
            'status': 'ok',
            'proxy': 'opencode',
            'target': TARGET_ORIGIN,
            **snapshot,
            'ts': time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime()),
        }, ensure_ascii=False, indent=2).encode()
        self.send_response(200)
        self.send_header('Content-Type', 'application/json')
        self.send_header('Content-Length', str(len(body)))
        self._send_cors_headers()
        self.end_headers()
        self.wfile.write(body)

    # ---- Prometheus-format metrics for monitoring integrations ----
    def _metrics_endpoint(self):
        """Expose metrics in Prometheus text exposition format."""
        with _stats_lock:
            total_req = _stats['total_requests']
            total_err = _stats['total_errors']
            total_bytes = _stats['total_bytes_sent']
            total_lat = _stats['total_latency_ms']
            total_retries = _stats['total_upstream_retries']
            avg_lat = round(total_lat / total_req) if total_req > 0 else 0
            sc = dict(_stats['status_codes'])
            errs = dict(_stats['errors'])
            tokens_in = _stats['total_tokens_in']
            tokens_out = _stats['total_tokens_out']
            tokens_reasoning = _stats['total_tokens_reasoning']
            tokens_total_val = _stats['total_tokens_total']
            started_at = _stats['started_at']
            # Snapshot per-model data to avoid nested lock acquisition below
            per_model_snapshot = {
                name: dict(mdata) for name, mdata in _stats['per_model'].items()
            }
        with _active_requests_lock:
            active = len(_active_requests)
        uptime = round(time.time() - started_at)
        lines = [
            '# HELP opencode_proxy_uptime_seconds Time since proxy start',
            '# TYPE opencode_proxy_uptime_seconds gauge',
            f'opencode_proxy_uptime_seconds {uptime}',
            '# HELP opencode_proxy_active_requests Currently in-flight requests',
            '# TYPE opencode_proxy_active_requests gauge',
            f'opencode_proxy_active_requests {active}',
            '# HELP opencode_proxy_requests_total Total proxied requests',
            '# TYPE opencode_proxy_requests_total counter',
            f'opencode_proxy_requests_total {total_req}',
            '# HELP opencode_proxy_errors_total Total proxy errors',
            '# TYPE opencode_proxy_errors_total counter',
            f'opencode_proxy_errors_total {total_err}',
            '# HELP opencode_proxy_bytes_sent_total Total bytes sent to clients',
            '# TYPE opencode_proxy_bytes_sent_total counter',
            f'opencode_proxy_bytes_sent_total {total_bytes}',
            '# HELP opencode_proxy_latency_ms_total Cumulative latency in ms',
            '# TYPE opencode_proxy_latency_ms_total counter',
            f'opencode_proxy_latency_ms_total {total_lat}',
            '# HELP opencode_proxy_avg_latency_ms Average latency per request',
            '# TYPE opencode_proxy_avg_latency_ms gauge',
            f'opencode_proxy_avg_latency_ms {avg_lat}',
            '# HELP opencode_proxy_upstream_retries_total Total upstream retries',
            '# TYPE opencode_proxy_upstream_retries_total counter',
            f'opencode_proxy_upstream_retries_total {total_retries}',
            '# HELP opencode_proxy_tokens_in_total Total prompt tokens processed',
            '# TYPE opencode_proxy_tokens_in_total counter',
            f'opencode_proxy_tokens_in_total {tokens_in}',
            '# HELP opencode_proxy_tokens_out_total Total completion tokens generated',
            '# TYPE opencode_proxy_tokens_out_total counter',
            f'opencode_proxy_tokens_out_total {tokens_out}',
            '# HELP opencode_proxy_tokens_reasoning_total Total reasoning tokens',
            '# TYPE opencode_proxy_tokens_reasoning_total counter',
            f'opencode_proxy_tokens_reasoning_total {tokens_reasoning}',
            '# HELP opencode_proxy_tokens_total_total Total tokens across all models',
            '# TYPE opencode_proxy_tokens_total_total counter',
            f'opencode_proxy_tokens_total_total {tokens_total_val}',
        ]
        # Per-status-code counters
        for code, count in sorted(sc.items()):
            lines.append(f'opencode_proxy_status_code_total{{status="{code}"}} {count}')
        # Per-error-type counters
        for etype, count in sorted(errs.items()):
            lines.append(f'opencode_proxy_error_type_total{{error="{etype}"}} {count}')
        # Per-model counters (uses pre-snapshotted data, no lock needed).
        # NOTE: Prometheus exposition format requires that # HELP / # TYPE
        # appear at most once per metric name. The old code declared them
        # inside the per-model loop, producing duplicate header lines that
        # strict parsers (promtool check) reject. The headers are now hoisted
        # out of the loop so each metric name is declared exactly once.
        if per_model_snapshot:
            lines.append('# HELP opencode_proxy_model_requests_total Requests per model')
            lines.append('# TYPE opencode_proxy_model_requests_total counter')
            for model_name in sorted(per_model_snapshot.keys()):
                m = per_model_snapshot[model_name]
                _mn = model_name.replace('\\', '\\\\').replace('"', '\\"')
                lines.append(f'opencode_proxy_model_requests_total{{model="{_mn}"}} {m["requests"]}')
            lines.append('# HELP opencode_proxy_model_tokens_total Tokens per model')
            lines.append('# TYPE opencode_proxy_model_tokens_total counter')
            for model_name in sorted(per_model_snapshot.keys()):
                m = per_model_snapshot[model_name]
                _mn = model_name.replace('\\', '\\\\').replace('"', '\\"')
                lines.append(f'opencode_proxy_model_tokens_total{{model="{_mn}"}} {m["tokens_total"]}')
            lines.append('# HELP opencode_proxy_model_errors_total Errors per model')
            lines.append('# TYPE opencode_proxy_model_errors_total counter')
            for model_name in sorted(per_model_snapshot.keys()):
                m = per_model_snapshot[model_name]
                _mn = model_name.replace('\\', '\\\\').replace('"', '\\"')
                lines.append(f'opencode_proxy_model_errors_total{{model="{_mn}"}} {m["errors"]}')
        body = '\n'.join(lines).encode('utf-8')
        self.send_response(200)
        self.send_header('Content-Type', 'text/plain; version=0.0.4')
        self.send_header('Content-Length', str(len(body)))
        self._send_cors_headers()
        self.end_headers()
        self.wfile.write(body)

    # ---- OPTIONS preflight handler ----
    def _cors_preflight(self):
        """Handle CORS preflight requests without forwarding to upstream."""
        request_id = uuid.uuid4().hex[:12]
        logger.info('[%s] CORS preflight %s %s', request_id, self.command, self.path)
        self.send_response(204)
        self._send_cors_headers()
        self.send_header('X-Request-Id', request_id)
        self.send_header('Content-Length', '0')
        self.end_headers()

    # ---- Actual proxy logic ----
    def _proxy(self):
        request_id = uuid.uuid4().hex[:12]
        n = int(self.headers.get('content-length') or 0)

        # Reject oversized requests early
        if n > MAX_BODY_BYTES:
            logger.warning('[%s] request body too large: %d bytes (limit %d)',
                           request_id, n, MAX_BODY_BYTES)
            out = json.dumps({
                'error': 'request_too_large',
                'detail': f'Max body size is {MAX_BODY_BYTES} bytes',
            }).encode()
            self.send_response(413)
            self.send_header('Content-Type', 'application/json')
            self.send_header('Content-Length', str(len(out)))
            self.send_header('X-Request-Id', request_id)
            self._send_cors_headers()
            self.end_headers()
            self.wfile.write(out)
            _record_stats(413, 0, 0, 'request_too_large')
            return

        body = self.rfile.read(n) if n else b''
        target = TARGET_ORIGIN + self.path

        # Build forwarded headers: strip hop-by-hop, inject request id for tracing
        headers = {
            k: v for k, v in self.headers.items()
            if k.lower() not in BLACKLIST_HEADERS
        }
        headers['X-Request-Id'] = request_id
        # Forward the real client IP so upstream can debug, rate-limit, and audit
        # actual clients rather than seeing only the proxy's loopback address.
        client_ip = self.client_address[0] if self.client_address else None
        if client_ip:
            existing_xff = self.headers.get('X-Forwarded-For', '')
            if existing_xff:
                headers['X-Forwarded-For'] = f'{existing_xff}, {client_ip}'
            else:
                headers['X-Forwarded-For'] = client_ip
        # Forward the original client's User-Agent so upstream can debug real clients
        # rather than seeing only the proxy's default UA from the session.
        ua = self.headers.get('User-Agent', '')
        if ua:
            headers['X-Forwarded-User-Agent'] = ua
        # Tell upstream the original protocol so it can build correct redirect URLs.
        headers['X-Forwarded-Proto'] = 'http'

        # Parse the request body once to: (1) inject stream_options.include_usage
        # for streaming requests so upstream sends token usage in the final SSE
        # chunk (OpenAI-compatible convention), (2) extract the model name for
        # per-model statistics, and (3) detect streaming.  Without usage injection
        # streaming responses carry zero token usage and stats undercount.
        _injected_usage_opt = False
        req_model = None
        _body_json = None
        is_streaming = False
        _usage_search_exhausted = False
        if body:
            try:
                _body_json = json.loads(body.decode('utf-8'))
            except Exception:
                _body_json = None
            if isinstance(_body_json, dict):
                req_model = _body_json.get('model')
                is_streaming = bool(_body_json.get('stream'))
                if is_streaming:
                    so = _body_json.get('stream_options')
                    if not isinstance(so, dict) or not so.get('include_usage'):
                        _body_json.setdefault('stream_options', {})['include_usage'] = True
                        body = json.dumps(_body_json, ensure_ascii=False).encode('utf-8')
                        headers['Content-Length'] = str(len(body))
                        _injected_usage_opt = True
                        logger.info('[%s] injected stream_options.include_usage for model=%s',
                                    request_id, req_model)
        rec = {
            'request_id': request_id,
            'ts': time.strftime('%Y-%m-%d %H:%M:%S'),
            'method': self.command,
            'path': self.path,
            'target': target,
            'headers': {
                k: ('[REDACTED]' if k.lower() in ('authorization', 'x-api-key') else v)
                for k, v in headers.items()
            },
        }
        bytes_sent = 0
        upstream_streaming = False
        _headers_sent = False
        t0 = time.monotonic()
        elapsed = 0
        error_type = None
        retries = 0
        resp_tokens = None  # {prompt_tokens, completion_tokens, total_tokens}

        try:
            # Body was already parsed above for stream_options injection;
            # populate rec for logging without re-parsing.
            if _body_json is not None:
                rec['request_json'] = _body_json
            elif body:
                rec['request_body'] = body[:5000].decode('utf-8', 'replace')

            # ---- Upstream request with retry for transient failures ----
            # The retry loop catches both retryable HTTP status codes (429,
            # 500–504) AND network-level transient exceptions (ConnectionError,
            # Timeout) that occur during the request() call itself.  Without
            # the inner try/except, a transient network blip (DNS hiccup, TCP
            # reset, connection refused) would propagate straight to the outer
            # exception handler and return an error to the client without
            # ever attempting a retry — defeating the purpose of the retry
            # configuration.
            resp = None
            for _attempt in range(UPSTREAM_MAX_RETRIES):
                try:
                    resp = _upstream_session.request(
                        self.command, target,
                        headers=headers, data=body,
                        timeout=(CONNECT_TIMEOUT, READ_TIMEOUT), stream=True,
                    )
                except (requests.exceptions.ConnectionError,
                        requests.exceptions.Timeout) as net_exc:
                    if _attempt < UPSTREAM_MAX_RETRIES - 1:
                        retries += 1
                        wait = UPSTREAM_RETRY_BACKOFF_BASE * (2 ** _attempt)
                        logger.info(
                            '[%s] upstream network retry %d/%d after %.1fs (%s, path=%s)',
                            request_id, retries, UPSTREAM_MAX_RETRIES - 1, wait,
                            type(net_exc).__name__, self.path,
                        )
                        time.sleep(wait)
                        continue
                    # Last attempt — re-raise so the outer handler produces
                    # the proper error response with sanitized details.
                    raise
                # Retry on transient upstream errors (429, 500–504)
                if (resp.status_code in _RETRYABLE_STATUSES
                        and _attempt < UPSTREAM_MAX_RETRIES - 1):
                    retries += 1
                    # Respect Retry-After header for 429 rate-limiting
                    retry_after = resp.headers.get('Retry-After')
                    if retry_after:
                        try:
                            wait = min(float(retry_after), 30)
                        except ValueError:
                            wait = UPSTREAM_RETRY_BACKOFF_BASE * (2 ** _attempt)
                    else:
                        wait = UPSTREAM_RETRY_BACKOFF_BASE * (2 ** _attempt)
                    logger.info(
                        '[%s] upstream retry %d/%d after %.1fs (status=%d, path=%s)',
                        request_id, retries, UPSTREAM_MAX_RETRIES - 1, wait,
                        resp.status_code, self.path,
                    )
                    resp.close()
                    time.sleep(wait)
                    continue
                break  # Non-retryable status or last attempt
            rec['status_code'] = resp.status_code
            rec['response_headers'] = dict(resp.headers.items())
            if retries:
                rec['retries'] = retries

            # Determine if upstream is actually streaming
            ct = resp.headers.get('content-type', '')
            upstream_streaming = ct.startswith(_STREAM_CT_PREFIXES) or is_streaming
            rec['streaming'] = upstream_streaming

            # Send response headers to client
            self.send_response(resp.status_code)
            self.send_header('X-Request-Id', request_id)
            # For HEAD requests, forward Content-Length but strip encoding headers
            _skip = {'content-encoding', 'transfer-encoding', 'connection'}
            if self.command != 'HEAD':
                _skip.add('content-length')
            for k, v in resp.headers.items():
                if k.lower() not in _skip:
                    self.send_header(k, v)

            if self.command == 'HEAD':
                # RFC 7231 §4.3.2: HEAD must not include a message body.
                # Upstream Content-Length was already forwarded above.
                self._send_cors_headers()
                self.end_headers()
                _headers_sent = True

            elif upstream_streaming:
                # Streaming: use Transfer-Encoding: chunked
                self.send_header('Transfer-Encoding', 'chunked')
                self._send_cors_headers()
                self.end_headers()
                _headers_sent = True

                # Forward chunks as they arrive, catching client disconnects
                # so we can close the upstream connection immediately instead
                # of letting it hang open until timeout.
                _stream_buf = b''
                try:
                    for chunk in resp.iter_content(chunk_size=8192):
                        if chunk:
                            self.wfile.write(f'{len(chunk):X}\r\n'.encode())
                            self.wfile.write(chunk)
                            self.wfile.write(b'\r\n')
                            self.wfile.flush()  # ensure immediate delivery for real-time SSE
                            bytes_sent += len(chunk)
                            # Accumulate to parse the final usage SSE chunk.
                            # OpenAI convention: the last data: line carries
                            # {"usage":{...}} when stream_options.include_usage is set.
                            # Stop buffering once we've captured usage to avoid
                            # unbounded memory growth on long streaming responses.
                            if resp_tokens is None and not _usage_search_exhausted:
                                _stream_buf += chunk
                                # Cap buffer at 256 KB to bound memory usage on
                                # long streams where upstream never sends usage.
                                # If the usage chunk hasn't arrived by 256 KB of
                                # SSE data, it's not coming — stop buffering.
                                if len(_stream_buf) > 262144:
                                    _stream_buf = b''
                                    _usage_search_exhausted = True
                                # Process complete SSE lines (delimited by \n)
                                while b'\n' in _stream_buf:
                                    line, _stream_buf = _stream_buf.split(b'\n', 1)
                                    line = line.strip()
                                    if line.startswith(b'data: '):
                                        payload = line[6:]
                                        if payload == b'[DONE]':
                                            continue
                                        try:
                                            sj = json.loads(payload)
                                            if isinstance(sj, dict) and 'usage' in sj:
                                                resp_tokens = sj['usage']
                                                _stream_buf = b''  # free memory
                                                if not req_model:
                                                    req_model = sj.get('model')
                                        except Exception:
                                            pass
                except (BrokenPipeError, ConnectionResetError):
                    error_type = 'client_disconnect'
                    logger.info(
                        '[%s] client disconnected mid-stream after %dB, closing upstream',
                        request_id, bytes_sent,
                    )
                except (_ChunkedEncError, requests.exceptions.ConnectionError):
                    error_type = 'upstream_disconnect'
                    logger.warning(
                        '[%s] upstream disconnected mid-stream after %dB (path=%s)',
                        request_id, bytes_sent, self.path,
                    )
                # Terminate chunked stream — send final 0-length chunk.
                # For upstream_disconnect this signals a truncated response
                # rather than letting the client wait indefinitely.
                if error_type != 'client_disconnect':
                    try:
                        self.wfile.write(b'0\r\n\r\n')
                    except (BrokenPipeError, ConnectionResetError):
                        pass  # client already gone, nothing more to do
            else:
                # Non-streaming: buffer and forward with Content-Length
                out = resp.content
                bytes_sent = len(out)
                rec['response_text'] = resp.text[:12000]
                # Extract token usage from non-streaming response
                try:
                    resp_json = json.loads(out)
                    if isinstance(resp_json, dict):
                        usage = resp_json.get('usage')
                        if isinstance(usage, dict):
                            resp_tokens = usage
                            # Also capture model from response if request didn't have it
                            if not req_model:
                                req_model = resp_json.get('model')
                except Exception:
                    pass
                self.send_header('Content-Length', str(len(out)))
                self._send_cors_headers()
                self.end_headers()
                _headers_sent = True
                self.wfile.write(out)

            elapsed = round((time.monotonic() - t0) * 1000)
            rec['bytes_sent'] = bytes_sent
            rec['elapsed_ms'] = elapsed

        except requests.exceptions.Timeout as e:
            rec['proxy_error'] = repr(e)
            elapsed = round((time.monotonic() - t0) * 1000)
            rec['elapsed_ms'] = elapsed
            # Distinguish connect timeout (failed to establish connection)
            # from read timeout (connection ok but response too slow).
            # requests.exceptions.ConnectTimeout is a subclass of Timeout
            # and specifically indicates a connection-phase failure.
            if isinstance(e, requests.exceptions.ConnectTimeout):
                timeout_type = 'connect_timeout'
            else:
                timeout_type = 'read_timeout'
            error_type = timeout_type
            logger.warning('[%s] upstream %s after %dms: %s %s — %s',
                           request_id, timeout_type, elapsed, self.command, self.path, e)
            if not _headers_sent:
                # Sanitize: do not expose internal exception repr to clients.
                # The request_id lets operators correlate with server-side logs.
                out = json.dumps(
                    {'error': f'upstream_{timeout_type}',
                     'detail': f'the upstream service did not respond within the timeout window',
                     'request_id': request_id},
                    ensure_ascii=False,
                ).encode('utf-8')
                self.send_response(504)
                self.send_header('Content-Type', 'application/json')
                self.send_header('Content-Length', str(len(out)))
                self.send_header('X-Request-Id', request_id)
                self._send_cors_headers()
                self.end_headers()
                self.wfile.write(out)

        except requests.exceptions.ConnectionError as e:
            rec['proxy_error'] = repr(e)
            elapsed = round((time.monotonic() - t0) * 1000)
            rec['elapsed_ms'] = elapsed
            error_type = 'connection_error'
            logger.error('[%s] upstream connection error: %s %s — %s',
                         request_id, self.command, self.path, e)
            if not _headers_sent:
                out = json.dumps(
                    {'error': 'upstream_unreachable',
                     'detail': 'unable to connect to the upstream service',
                     'request_id': request_id},
                    ensure_ascii=False,
                ).encode('utf-8')
                self.send_response(502)
                self.send_header('Content-Type', 'application/json')
                self.send_header('Content-Length', str(len(out)))
                self.send_header('X-Request-Id', request_id)
                self._send_cors_headers()
                self.end_headers()
                self.wfile.write(out)

        except (BrokenPipeError, ConnectionResetError) as e:
            rec['proxy_error'] = repr(e)
            elapsed = round((time.monotonic() - t0) * 1000)
            rec['elapsed_ms'] = elapsed
            error_type = 'client_disconnect'
            logger.warning('[%s] client disconnected after %dms: %s %s',
                           request_id, elapsed, self.command, self.path)

        except Exception as e:
            rec['proxy_error'] = repr(e)
            elapsed = round((time.monotonic() - t0) * 1000)
            rec['elapsed_ms'] = elapsed
            error_type = 'proxy_error'
            logger.exception('[%s] proxy error for %s %s', request_id, self.command, self.path)
            if not _headers_sent:
                out = json.dumps(
                    {'error': 'proxy_error',
                     'detail': 'an internal proxy error occurred',
                     'request_id': request_id},
                    ensure_ascii=False,
                ).encode('utf-8')
                self.send_response(502)
                self.send_header('Content-Type', 'application/json')
                self.send_header('Content-Length', str(len(out)))
                self.send_header('X-Request-Id', request_id)
                self._send_cors_headers()
                self.end_headers()
                self.wfile.write(out)

        finally:
            # Ensure upstream connection is always released back to the pool,
            # even if an exception cut the streaming loop short.
            _resp = locals().get('resp')
            if _resp is not None:
                try:
                    _resp.close()
                except Exception:
                    pass
            elapsed_ms = rec.get('elapsed_ms', round((time.monotonic() - t0) * 1000))
            stream_tag = ' [streaming]' if upstream_streaming else ''
            retry_tag = f' [retried {retries}x]' if retries else ''
            # Structured log line (human-readable in rotated log)
            log_line = (
                f"[{request_id}] "
                f"{self.command} {self.path} -> "
                f"{rec.get('status_code', 'ERR')} "
                f"{bytes_sent}B {elapsed_ms}ms"
                f"{retry_tag}{stream_tag}"
                f" ({rec.get('proxy_error', 'ok')})"
            )
            if rec.get('proxy_error'):
                logger.warning(log_line)
            else:
                logger.info(log_line)
            _record_stats(rec.get('status_code', 0), bytes_sent, elapsed_ms,
                          error_type, retries, req_model,
                          (resp_tokens or {}).get('prompt_tokens', 0),
                          (resp_tokens or {}).get('completion_tokens', 0),
                          ((resp_tokens or {}).get('completion_tokens_details') or {}).get('reasoning_tokens', 0),
                          )

    # ---- Dispatchers ----
    def do_GET(self):     self._dispatch()
    def do_POST(self):    self._dispatch()
    def do_PUT(self):     self._dispatch()
    def do_DELETE(self):  self._dispatch()
    def do_PATCH(self):   self._dispatch()
    def do_OPTIONS(self): self._dispatch()
    def do_HEAD(self):    self._dispatch()

    def _dispatch(self):
        if self.command == 'OPTIONS':
            self._cors_preflight()
        elif self.path in HEALTH_PATHS:
            self._health()
        elif self.path in STATS_PATHS:
            self._stats_endpoint()
        elif self.path in METRICS_PATHS:
            self._metrics_endpoint()
        else:
            self._proxy()


# ---------------------------------------------------------------------------
# Graceful shutdown on SIGINT / SIGTERM
# ---------------------------------------------------------------------------
_server = None
_start_time = None

def _shutdown(sig, frame):
    global _server
    if _server:
        uptime = round(time.monotonic() - _start_time) if _start_time else 0
        with _active_requests_lock:
            active = len(_active_requests)
        logger.info('shutting down (signal=%s, uptime=%ds, requests=%d, active=%d)',
                     sig, uptime, _stats['total_requests'], active)
        print(f'shutting down (signal={sig}, uptime={uptime}s, '
              f'requests={_stats["total_requests"]}, active={active})', flush=True)
        # Stop periodic auto-save first so it doesn't race with the final save
        _stats_saver_stop.set()
        # Stop accepting new connections
        _server.shutdown()
        # Drain in-flight requests: wait up to 30s for active threads to finish
        drain_timeout = int(os.environ.get('OPENCODE_PROXY_DRAIN_TIMEOUT', '30'))
        if active > 0 and drain_timeout > 0:
            logger.info('draining %d active request(s) (timeout=%ds)...', active, drain_timeout)
            deadline = time.monotonic() + drain_timeout
            while time.monotonic() < deadline:
                with _active_requests_lock:
                    remaining = len(_active_requests)
                if remaining == 0:
                    break
                logger.info('draining: %d request(s) still active...', remaining)
                time.sleep(1)
            with _active_requests_lock:
                remaining = len(_active_requests)
            if remaining:
                logger.warning('drain timeout: %d request(s) still active, forcing close',
                               remaining)
            else:
                logger.info('drain complete: all requests finished')
        else:
            logger.info('no active requests, skipping drain')
    # Persist stats so totals carry over to the next run
    _save_stats()
    # Remove PID file now that we're shutting down
    _remove_pid_file()
    # Close persistent connections to upstream
    _upstream_session.close()

signal.signal(signal.SIGINT, _shutdown)
signal.signal(signal.SIGTERM, _shutdown)


if __name__ == '__main__':
    port = int(os.environ.get('OPENCODE_PROXY_PORT', '18082'))
    _start_time = time.monotonic()
    _stats['started_at'] = time.time()
    _load_stats()
    _write_pid_file()
    # Start periodic stats auto-save daemon (protects against crash data loss)
    if STATS_SAVE_INTERVAL > 0:
        _saver = threading.Thread(target=_stats_auto_save_loop, daemon=True)
        _saver.start()
        logger.info('stats auto-save enabled (interval=%ds)', STATS_SAVE_INTERVAL)
    logger.info('opencode proxy listening on 127.0.0.1:%d -> %s (max_body=%dMB, cors=%s, retry=%d)',
                port, TARGET_ORIGIN, MAX_BODY_BYTES // (1024 * 1024), CORS_ORIGIN,
                UPSTREAM_MAX_RETRIES)
    print(f'opencode proxy listening on 127.0.0.1:{port} -> {TARGET_ORIGIN} '
          f'(max_body={MAX_BODY_BYTES // (1024 * 1024)}MB, cors={CORS_ORIGIN}, '
          f'retry={UPSTREAM_MAX_RETRIES})', flush=True)
    _server = GracefulHTTPServer(('127.0.0.1', port), H)
    _server.serve_forever()