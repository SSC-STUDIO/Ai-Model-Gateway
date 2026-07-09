"""Mock OpenAI server on :9999 to prove the SSRF guard behavior.
Listens dual-stack (::/::1 and 127.0.0.1). Logs every request so we can
observe health probes and forwarded chat requests landing on localhost.
"""
import http.server, socketserver, socket, json, time, sys, os

LOG = os.path.join(os.path.dirname(os.path.abspath(__file__)), "out", "mock-ssrf.log")

def log(msg):
    line = f"{time.strftime('%H:%M:%S')} {msg}"
    print(line, flush=True)
    with open(LOG, "a", encoding="utf-8") as f:
        f.write(line + "\n")

models = [
    {"id": "ssrf-local-model", "object": "model", "owned_by": "mock"},
    {"id": "ssrf-loopback-model", "object": "model", "owned_by": "mock"},
    {"id": "ssrf-private-model", "object": "model", "owned_by": "mock"},
]

class Handler(http.server.BaseHTTPRequestHandler):
    def _send(self, code, body):
        b = json.dumps(body).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(b)))
        self.end_headers()
        self.wfile.write(b)
    def do_GET(self):
        log(f"GET {self.path} from {self.client_address[0]}")
        if self.path.startswith("/v1/models"):
            self._send(200, {"object": "list", "data": models})
        elif self.path == "/-/health" or self.path == "/health":
            self._send(200, {"status": "ok"})
        else:
            self._send(404, {"error": {"message": "not found", "type": "mock"}})
    def do_POST(self):
        length = int(self.headers.get("Content-Length", 0))
        raw = self.rfile.read(length) if length else b""
        try:
            payload = json.loads(raw) if raw else {}
        except Exception:
            payload = {}
        model = payload.get("model", "?")
        log(f"POST {self.path} model={model} from {self.client_address[0]}")
        now = int(time.time())
        if self.path.startswith("/v1/chat/completions"):
            self._send(200, {
                "id": "chatcmpl-mock", "object": "chat.completion", "created": now,
                "model": model,
                "choices": [{"index": 0, "message": {"role": "assistant",
                    "content": "MOCK SSRF HIT: gateway forwarded to localhost upstream on :9999"},
                    "finish_reason": "stop"}],
                "usage": {"prompt_tokens": 2, "completion_tokens": 9, "total_tokens": 11},
            })
        elif self.path.startswith("/v1/responses"):
            self._send(200, {
                "id": "resp_mock", "object": "response", "created_at": now,
                "model": model, "status": "completed",
                "output": [{"type": "message", "role": "assistant",
                    "content": [{"type": "output_text",
                        "text": "MOCK SSRF HIT via /v1/responses: localhost upstream :9999"}]}],
                "usage": {"input_tokens": 2, "output_tokens": 9, "total_tokens": 11},
            })
        elif self.path.startswith("/v1/messages"):
            self._send(200, {
                "id": "msg_mock", "type": "message", "role": "assistant", "model": model,
                "content": [{"type": "text", "text": "MOCK SSRF HIT via /v1/messages: localhost upstream :9999"}],
                "stop_reason": "end_turn", "usage": {"input_tokens": 2, "output_tokens": 9},
            })
        else:
            self._send(404, {"error": {"message": "not found", "type": "mock"}})
    def log_message(self, *a):
        pass  # silence stderr access log; we log our own

class DualStackServer(socketserver.ThreadingMixIn, http.server.HTTPServer):
    address_family = socket.AF_INET6
    allow_reuse_address = True
    daemon_threads = True
    def server_bind(self):
        self.socket.setsockopt(socket.IPPROTO_IPV6, socket.IPV6_V6ONLY, 0)
        http.server.HTTPServer.server_bind(self)

if __name__ == "__main__":
    port = int(sys.argv[1]) if len(sys.argv) > 1 else 9999
    s = DualStackServer(("::", port), Handler)
    log(f"mock SSRF server listening on [::]:{port} (dual-stack, V6ONLY=0)")
    try:
        s.serve_forever()
    except KeyboardInterrupt:
        log("shutting down")
        s.shutdown()
