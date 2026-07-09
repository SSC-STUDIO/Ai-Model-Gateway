from http.server import ThreadingHTTPServer, BaseHTTPRequestHandler
import requests, os, json, time, pathlib, sys, logging, signal, uuid, threading, random
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
# Per-IP concurrent request limit.  When a single client IP exceeds this many
# simultaneous in-flight requests, additional requests get 429 Too Many Requests
# with a Retry-After header.  This prevents a single client (buggy or malicious)
# from monopolising the proxy's connection pool and starving other clients.
# 0 = unlimited (disabled).
MAX_CONCURRENT_PER_IP = int(os.environ.get('OPENCODE_PROXY_MAX_CONCURRENT_PER_IP', '0'))
# When a client is throttled, how many seconds to suggest they wait.
THROTTLE_RETRY_AFTER_SEC = int(os.environ.get('OPENCODE_PROXY_THROTTLE_RETRY_AFTER', '5'))

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
READINESS_PATHS = frozenset({'/ready', '/-/ready'})
# Admin / monitoring paths served by the proxy (not forwarded upstream)
STATS_PATHS = frozenset({'/stats', '/-/stats'})
METRICS_PATHS = frozenset({'/metrics', '/-/metrics', '/prometheus'})
# Model discovery: OpenAI-compatible /v1/models endpoint.  Served locally
# from proxy stats instead of forwarding upstream, so clients can discover
# which models have been proxied even when the upstream doesn't implement
# this endpoint.
MODELS_PATHS = frozenset({'/v1/models', '/-/models'})
BLACKLIST_HEADERS = frozenset({
    'host', 'content-length', 'connection', 'accept-encoding',
    'transfer-encoding',
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
    'total_bytes_received': 0,
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
            _stats['total_bytes_received'] += saved.get('total_bytes_received', 0)
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
                    'total_latency_ms': 0, 'bytes_sent': 0, 'bytes_received': 0,
                })
                m['requests'] += mdata.get('requests', 0)
                m['errors'] += mdata.get('errors', 0)
                m['tokens_in'] += mdata.get('tokens_in', 0)
                m['tokens_out'] += mdata.get('tokens_out', 0)
                m['tokens_reasoning'] += mdata.get('tokens_reasoning', 0)
                m['tokens_total'] += mdata.get('tokens_total', 0)
                m['total_latency_ms'] += mdata.get('total_latency_ms', 0)
                m['bytes_sent'] += mdata.get('bytes_sent', 0)
                m['bytes_received'] += mdata.get('bytes_received', 0)
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

# ---------------------------------------------------------------------------
# Per-IP concurrent request tracking (DoS protection)
# ---------------------------------------------------------------------------
_ip_concurrent = {}          # {ip: count}
_ip_concurrent_lock = threading.Lock()
_ip_throttle_count = 0       # total throttle events (for stats)


def _ip_request_start(client_ip):
    """Register a new in-flight request from client_ip.
    Returns True if allowed, False if per-IP limit exceeded."""
    global _ip_throttle_count
    if not client_ip or MAX_CONCURRENT_PER_IP <= 0:
        return True
    with _ip_concurrent_lock:
        count = _ip_concurrent.get(client_ip, 0) + 1
        if count > MAX_CONCURRENT_PER_IP:
            _ip_throttle_count += 1
            return False
        _ip_concurrent[client_ip] = count
        return True


def _ip_request_end(client_ip):
    """Deregister a completed request from client_ip."""
    if not client_ip or MAX_CONCURRENT_PER_IP <= 0:
        return
    with _ip_concurrent_lock:
        count = _ip_concurrent.get(client_ip, 1) - 1
        if count <= 0:
            _ip_concurrent.pop(client_ip, None)
        else:
            _ip_concurrent[client_ip] = count


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
                  model=None, tokens_in=0, tokens_out=0, tokens_reasoning=0,
                  bytes_received=0):
    with _stats_lock:
        _stats['total_requests'] += 1
        _stats['total_bytes_sent'] += bytes_sent
        _stats['total_bytes_received'] += bytes_received
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
                'total_latency_ms': 0, 'bytes_sent': 0, 'bytes_received': 0,
            })
            m['requests'] += 1
            m['bytes_sent'] += bytes_sent
            m['bytes_received'] += bytes_received
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
            total_bytes_recv = _stats['total_bytes_received']
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
        with _ip_concurrent_lock:
            ip_throttle_total = _ip_throttle_count
            ip_unique = len(_ip_concurrent)
        body = json.dumps({
            'status': 'ok',
            'proxy': 'opencode',
            'target': TARGET_ORIGIN,
            'uptime_seconds': uptime,
            'active_requests': active,
            'ip_concurrent_limit': MAX_CONCURRENT_PER_IP,
            'ip_throttled_total': ip_throttle_total,
            'ip_unique_active': ip_unique,
            'stats': {
                'total_requests': total,
                'total_errors': total_errors,
                'total_bytes_sent': total_bytes,
                'total_bytes_received': total_bytes_recv,
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

    # ---- Readiness probe (separate from liveness) ----
    def _ready(self):
        """Readiness check: returns 200 when the proxy can serve requests,
        503 when shutting down or unable to reach upstream.

        Docker/K8s readiness probes should use this endpoint instead of
        /health so that a draining or upstream-disconnected proxy is removed
        from the load balancer without being restarted.
        """
        # Check the shutdown signal — if we're draining, we're not ready.
        if _stats_saver_stop.is_set():
            body = json.dumps({
                'status': 'draining',
                'ts': time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime()),
            }).encode()
            self.send_response(503)
            self.send_header('Content-Type', 'application/json')
            self.send_header('Content-Length', str(len(body)))
            self._send_cors_headers()
            self.end_headers()
            self.wfile.write(body)
            return

        with _active_requests_lock:
            active = len(_active_requests)
        body = json.dumps({
            'status': 'ready',
            'active_requests': active,
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
                'total_bytes_received': _stats['total_bytes_received'],
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
        with _ip_concurrent_lock:
            snapshot['ip_concurrent_limit'] = MAX_CONCURRENT_PER_IP
            snapshot['ip_throttled_total'] = _ip_throttle_count
            snapshot['ip_unique_active'] = len(_ip_concurrent)
            # Include per-IP breakdown (top 20 by concurrency) for debugging
            _ip_sorted = sorted(_ip_concurrent.items(), key=lambda x: -x[1])[:20]
            snapshot['ip_concurrent_detail'] = {ip: cnt for ip, cnt in _ip_sorted}
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

    # ---- OpenAI-compatible model discovery endpoint ----
    def _models_endpoint(self):
        """Return available models in OpenAI /v1/models format.

        Provides an OpenAI-compatible model catalog from proxy stats so
        clients can discover which models have been proxied.  This is
        particularly useful when the upstream doesn't implement /v1/models
        or when the proxy bridges multiple providers with different model
        names.
        """
        with _stats_lock:
            per_model = {
                name: dict(mdata) for name, mdata in _stats['per_model'].items()
            }
        models_list = []
        for model_name in sorted(per_model.keys()):
            m = per_model[model_name]
            models_list.append({
                'id': model_name,
                'object': 'model',
                'created': int(_stats['started_at']),
                'owned_by': 'opencode-proxy',
                'requests': m['requests'],
                'errors': m['errors'],
                'tokens_total': m['tokens_total'],
            })
        body = json.dumps({
            'object': 'list',
            'data': models_list,
        }, ensure_ascii=False).encode('utf-8')
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
            total_bytes_recv = _stats['total_bytes_received']
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
        # Snapshot per-IP concurrent data for the throttle gauge
        with _ip_concurrent_lock:
            ip_throttle_total = _ip_throttle_count
            ip_active = sum(_ip_concurrent.values())
            ip_unique = len(_ip_concurrent)
        lines = [
            '# HELP opencode_proxy_uptime_seconds Time since proxy start',
            '# TYPE opencode_proxy_uptime_seconds gauge',
            f'opencode_proxy_uptime_seconds {uptime}',
            '# HELP opencode_proxy_active_requests Currently in-flight requests',
            '# TYPE opencode_proxy_active_requests gauge',
            f'opencode_proxy_active_requests {active}',
            '# HELP opencode_proxy_ip_throttled_total Total requests rejected due to per-IP concurrency limit',
            '# TYPE opencode_proxy_ip_throttled_total counter',
            f'opencode_proxy_ip_throttled_total {ip_throttle_total}',
            '# HELP opencode_proxy_ip_active_concurrent Currently in-flight requests grouped by client IP',
            '# TYPE opencode_proxy_ip_active_concurrent gauge',
            f'opencode_proxy_ip_active_concurrent {ip_active}',
            '# HELP opencode_proxy_ip_unique_active Number of unique client IPs with active requests',
            '# TYPE opencode_proxy_ip_unique_active gauge',
            f'opencode_proxy_ip_unique_active {ip_unique}',
            '# HELP opencode_proxy_requests_total Total proxied requests',
            '# TYPE opencode_proxy_requests_total counter',
            f'opencode_proxy_requests_total {total_req}',
            '# HELP opencode_proxy_errors_total Total proxy errors',
            '# TYPE opencode_proxy_errors_total counter',
            f'opencode_proxy_errors_total {total_err}',
            '# HELP opencode_proxy_bytes_sent_total Total bytes sent to clients',
            '# TYPE opencode_proxy_bytes_sent_total counter',
            f'opencode_proxy_bytes_sent_total {total_bytes}',
            '# HELP opencode_proxy_bytes_received_total Total bytes received from clients',
            '# TYPE opencode_proxy_bytes_received_total counter',
            f'opencode_proxy_bytes_received_total {total_bytes_recv}',
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
        if sc:
            lines.append('# HELP opencode_proxy_status_code_total Requests by HTTP status code')
            lines.append('# TYPE opencode_proxy_status_code_total counter')
        for code, count in sorted(sc.items()):
            lines.append(f'opencode_proxy_status_code_total{{status="{code}"}} {count}')
        # Per-error-type counters
        if errs:
            lines.append('# HELP opencode_proxy_error_type_total Errors by type')
            lines.append('# TYPE opencode_proxy_error_type_total counter')
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
            # Per-model latency: cumulative ms and average gauge, enabling
            # latency-over-time dashboards per model in Grafana.
            lines.append('# HELP opencode_proxy_model_latency_ms_total Cumulative upstream latency per model in ms')
            lines.append('# TYPE opencode_proxy_model_latency_ms_total counter')
            for model_name in sorted(per_model_snapshot.keys()):
                m = per_model_snapshot[model_name]
                _mn = model_name.replace('\\', '\\\\').replace('"', '\\"')
                lines.append(f'opencode_proxy_model_latency_ms_total{{model="{_mn}"}} {m["total_latency_ms"]}')
            # Per-model bytes sent/received — enables bandwidth cost analysis
            # per model in Grafana. Without these, operators can see aggregate
            # bandwidth but cannot attribute it to individual models.
            lines.append('# HELP opencode_proxy_model_bytes_sent_total Bytes sent to clients per model')
            lines.append('# TYPE opencode_proxy_model_bytes_sent_total counter')
            for model_name in sorted(per_model_snapshot.keys()):
                m = per_model_snapshot[model_name]
                _mn = model_name.replace('\\', '\\\\').replace('"', '\\"')
                lines.append(f'opencode_proxy_model_bytes_sent_total{{model="{_mn}"}} {m["bytes_sent"]}')
            lines.append('# HELP opencode_proxy_model_bytes_received_total Bytes received from clients per model')
            lines.append('# TYPE opencode_proxy_model_bytes_received_total counter')
            for model_name in sorted(per_model_snapshot.keys()):
                m = per_model_snapshot[model_name]
                _mn = model_name.replace('\\', '\\\\').replace('"', '\\"')
                lines.append(f'opencode_proxy_model_bytes_received_total{{model="{_mn}"}} {m["bytes_received"]}')
            lines.append('# HELP opencode_proxy_model_avg_latency_ms Average latency per request per model')
            lines.append('# TYPE opencode_proxy_model_avg_latency_ms gauge')
            for model_name in sorted(per_model_snapshot.keys()):
                m = per_model_snapshot[model_name]
                _mn = model_name.replace('\\', '\\\\').replace('"', '\\"')
                avg = round(m['total_latency_ms'] / m['requests']) if m['requests'] > 0 else 0
                lines.append(f'opencode_proxy_model_avg_latency_ms{{model="{_mn}"}} {avg}')
        # Prometheus text exposition format requires a trailing newline after
        # the last line. Without it, strict parsers (e.g. promtool) emit a
        # warning and some scrape configs silently drop the final metric.
        body = '\n'.join(lines).encode('utf-8') + b'\n'
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

    # ---- Read chunked request body (Transfer-Encoding: chunked) ----
    def _read_chunked_body(self):
        """Read an HTTP/1.1 chunked request body from self.rfile.

        Returns the decoded body as bytes, or None if the chunked stream
        is malformed (in which case a 400 response has already been sent).
        """
        chunks = []
        total = 0
        try:
            while True:
                line = self.rfile.readline()
                if not line:
                    break
                # Chunk size line: hex digits, optionally followed by ;ext
                line = line.strip()
                if not line:
                    continue
                semi = line.find(b';')
                size_str = line[:semi] if semi >= 0 else line
                try:
                    chunk_size = int(size_str, 16)
                except ValueError:
                    logger.warning('malformed chunk size: %r', line)
                    out = json.dumps({
                        'error': 'bad_request',
                        'detail': 'malformed chunked transfer encoding',
                    }).encode('utf-8')
                    self.send_response(400)
                    self.send_header('Content-Type', 'application/json')
                    self.send_header('Content-Length', str(len(out)))
                    self._send_cors_headers()
                    self.end_headers()
                    self.wfile.write(out)
                    return None
                if chunk_size == 0:
                    # Last chunk: read trailing CRLF (or trailer headers)
                    while True:
                        trailer = self.rfile.readline()
                        if trailer in (b'\r\n', b'\n', b''):
                            break
                    break
                # Read chunk data + trailing CRLF
                chunk_data = self.rfile.read(chunk_size)
                if len(chunk_data) < chunk_size:
                    logger.warning('chunked body truncated at chunk size %d', chunk_size)
                    out = json.dumps({
                        'error': 'bad_request',
                        'detail': 'chunked body truncated',
                    }).encode('utf-8')
                    self.send_response(400)
                    self.send_header('Content-Type', 'application/json')
                    self.send_header('Content-Length', str(len(out)))
                    self._send_cors_headers()
                    self.end_headers()
                    self.wfile.write(out)
                    return None
                self.rfile.read(2)  # consume trailing \r\n
                chunks.append(chunk_data)
                total += chunk_size
            return b''.join(chunks)
        except Exception as exc:
            logger.warning('error reading chunked body: %s', exc)
            out = json.dumps({
                'error': 'bad_request',
                'detail': 'error reading chunked request body',
            }).encode('utf-8')
            try:
                self.send_response(400)
                self.send_header('Content-Type', 'application/json')
                self.send_header('Content-Length', str(len(out)))
                self._send_cors_headers()
                self.end_headers()
                self.wfile.write(out)
            except Exception:
                pass
            return None

    # ---- Actual proxy logic ----
    def _proxy(self):
        request_id = uuid.uuid4().hex[:12]
        client_ip = self.client_address[0] if self.client_address else None

        # Parse Content-Length defensively: a malformed or negative value
        # must produce a clean 400 Bad Request, not a 502 from the generic
        # exception handler.  Clients sending "abc", "-1", or "0x10" would
        # otherwise crash int() → ValueError → 502 "proxy_error".
        _raw_cl = self.headers.get('content-length')
        n = 0
        if _raw_cl:
            try:
                n = int(_raw_cl.strip())
            except ValueError:
                logger.warning('[%s] malformed Content-Length: %r', request_id, _raw_cl)
                out = json.dumps({
                    'error': 'bad_request',
                    'detail': 'Content-Length header is not a valid integer',
                    'request_id': request_id,
                }).encode('utf-8')
                self.send_response(400)
                self.send_header('Content-Type', 'application/json')
                self.send_header('Content-Length', str(len(out)))
                self.send_header('X-Request-Id', request_id)
                self._send_cors_headers()
                self.end_headers()
                self.wfile.write(out)
                _record_stats(400, 0, 0, 'bad_content_length')
                return
            if n < 0:
                logger.warning('[%s] negative Content-Length: %d', request_id, n)
                out = json.dumps({
                    'error': 'bad_request',
                    'detail': 'Content-Length must be non-negative',
                    'request_id': request_id,
                }).encode('utf-8')
                self.send_response(400)
                self.send_header('Content-Type', 'application/json')
                self.send_header('Content-Length', str(len(out)))
                self.send_header('X-Request-Id', request_id)
                self._send_cors_headers()
                self.end_headers()
                self.wfile.write(out)
                _record_stats(400, 0, 0, 'bad_content_length')
                return

        # Per-IP concurrency throttle: protect the proxy from a single
        # client monopolising the connection pool.
        if not _ip_request_start(client_ip):
            logger.warning('[%s] per-IP throttle: %s exceeded %d concurrent',
                           request_id, client_ip, MAX_CONCURRENT_PER_IP)
            out = json.dumps({
                'error': 'too_many_concurrent_requests',
                'detail': f'Your IP has {MAX_CONCURRENT_PER_IP} concurrent requests in flight. '
                          f'Please retry after {THROTTLE_RETRY_AFTER_SEC}s.',
                'request_id': request_id,
            }).encode('utf-8')
            self.send_response(429)
            self.send_header('Content-Type', 'application/json')
            self.send_header('Content-Length', str(len(out)))
            self.send_header('Retry-After', str(THROTTLE_RETRY_AFTER_SEC))
            self.send_header('X-Request-Id', request_id)
            self._send_cors_headers()
            self.end_headers()
            self.wfile.write(out)
            _record_stats(429, 0, 0, 'ip_throttled')
            return

        _ip_done = False
        def _release_ip():
            nonlocal _ip_done
            if not _ip_done:
                _ip_request_end(client_ip)
                _ip_done = True

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

        # Read the request body.  Two cases:
        # 1. Content-Length present (n > 0): straightforward read.
        # 2. Transfer-Encoding: chunked (no Content-Length): the client
        #    sends a chunked stream.  http.server's BaseHTTPRequestHandler
        #    does NOT auto-decode chunked request bodies, so we must read
        #    chunk-by-chunk ourselves.  Without this, clients using chunked
        #    encoding (curl -T, some HTTP/2 → HTTP/1.1 proxies) would have
        #    their body silently dropped (n=0 → body=b'').
        _te = (self.headers.get('Transfer-Encoding') or '').lower()
        if n > 0:
            body = self.rfile.read(n)
        elif 'chunked' in _te:
            body = self._read_chunked_body()
            if body is None:
                # Malformed chunked encoding — _read_chunked_body already
                # sent a 400 response and logged the error.
                _release_ip()
                _record_stats(400, 0, 0, 'bad_chunked_encoding')
                return
            # Apply the oversized check to chunked bodies too
            if len(body) > MAX_BODY_BYTES:
                logger.warning('[%s] chunked body too large: %d bytes (limit %d)',
                               request_id, len(body), MAX_BODY_BYTES)
                out = json.dumps({
                    'error': 'request_too_large',
                    'detail': f'Max body size is {MAX_BODY_BYTES} bytes',
                }).encode('utf-8')
                self.send_response(413)
                self.send_header('Content-Type', 'application/json')
                self.send_header('Content-Length', str(len(out)))
                self.send_header('X-Request-Id', request_id)
                self._send_cors_headers()
                self.end_headers()
                self.wfile.write(out)
                _record_stats(413, 0, 0, 'request_too_large')
                _release_ip()
                return
        else:
            body = b''
        target = TARGET_ORIGIN + self.path

        # Build forwarded headers: strip hop-by-hop, inject request id for tracing
        headers = {
            k: v for k, v in self.headers.items()
            if k.lower() not in BLACKLIST_HEADERS
        }
        # When the client used Transfer-Encoding: chunked and we de-chunked
        # the body, strip the hop-by-hop header and set the real Content-Length
        # so the upstream sees a well-formed Content-Length-delimited request.
        if 'chunked' in _te:
            headers.pop('Transfer-Encoding', None)
            headers.pop('transfer-encoding', None)
            headers['Content-Length'] = str(len(body))
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
                        # Full jitter: random between 0 and exponential base.
                        # Prevents thundering herd when multiple clients retry
                        # simultaneously after an upstream recovery.
                        base = UPSTREAM_RETRY_BACKOFF_BASE * (2 ** _attempt)
                        wait = random.uniform(0, base)
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
                            base = UPSTREAM_RETRY_BACKOFF_BASE * (2 ** _attempt)
                            wait = random.uniform(0, base)
                    else:
                        # Full jitter to prevent thundering herd on recovery
                        base = UPSTREAM_RETRY_BACKOFF_BASE * (2 ** _attempt)
                        wait = random.uniform(0, base)
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
            # Release the per-IP concurrency slot.
            _release_ip()
            elapsed_ms = rec.get('elapsed_ms', round((time.monotonic() - t0) * 1000))
            stream_tag = ' [streaming]' if upstream_streaming else ''
            retry_tag = f' [retried {retries}x]' if retries else ''
            model_tag = f' [{req_model}]' if req_model else ''
            # Structured log line (human-readable in rotated log)
            log_line = (
                f"[{request_id}] "
                f"{self.command} {self.path}{model_tag} -> "
                f"{rec.get('status_code', 'ERR')} "
                f"req={len(body)}B resp={bytes_sent}B {elapsed_ms}ms"
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
                          bytes_received=len(body),
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
        elif self.path in READINESS_PATHS:
            self._ready()
        elif self.path in STATS_PATHS:
            self._stats_endpoint()
        elif self.path in MODELS_PATHS:
            self._models_endpoint()
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
        # Phase 1: Stop accepting new connections (non-blocking).
        # Use a background thread so the drain loop can proceed immediately.
        _shutdown_thread = threading.Thread(target=_server.shutdown, daemon=True)
        _shutdown_thread.start()
        # Phase 2: Drain in-flight requests before closing the socket.
        # This must happen BEFORE server_close() so active connections can
        # complete their responses.  Without drain, server_close() forces
        # TCP RST on in-flight requests, breaking streaming and long-polling.
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
        # Wait for shutdown thread to complete (socket stopped accepting)
        _shutdown_thread.join(timeout=5)
    # Persist stats so totals carry over to the next run
    _save_stats()
    # Remove PID file now that we're shutting down
    _remove_pid_file()
    # Close listening socket and persistent connections to upstream
    if _server:
        try:
            _server.server_close()
        except Exception:
            pass
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
    logger.info('opencode proxy listening on 127.0.0.1:%d -> %s (max_body=%dMB, cors=%s, retry=%d, ip_limit=%d)',
                port, TARGET_ORIGIN, MAX_BODY_BYTES // (1024 * 1024), CORS_ORIGIN,
                UPSTREAM_MAX_RETRIES, MAX_CONCURRENT_PER_IP)
    print(f'opencode proxy listening on 127.0.0.1:{port} -> {TARGET_ORIGIN} '
          f'(max_body={MAX_BODY_BYTES // (1024 * 1024)}MB, cors={CORS_ORIGIN}, '
          f'retry={UPSTREAM_MAX_RETRIES}, ip_limit={MAX_CONCURRENT_PER_IP})', flush=True)
    _server = GracefulHTTPServer(('127.0.0.1', port), H)
    _server.serve_forever()