from http.server import ThreadingHTTPServer, BaseHTTPRequestHandler
import requests, os, json, time, pathlib, sys
LOG = pathlib.Path(os.environ.get(
    'OPENCODE_PROXY_LOG',
    str(pathlib.Path(__file__).resolve().parent / 'opencode-proxy.log')
))
TARGET_ORIGIN = 'https://opencode.ai'
class H(BaseHTTPRequestHandler):
    protocol_version = 'HTTP/1.1'
    def log_message(self, fmt, *args):
        return
    def do_GET(self): self.handle_all()
    def do_POST(self): self.handle_all()
    def do_OPTIONS(self): self.handle_all()
    def handle_all(self):
        n = int(self.headers.get('content-length') or 0)
        body = self.rfile.read(n) if n else b''
        target = TARGET_ORIGIN + self.path
        headers = {k:v for k,v in self.headers.items() if k.lower() not in ('host','content-length','connection','accept-encoding')}
        rec = {'ts':time.strftime('%Y-%m-%d %H:%M:%S'), 'method':self.command, 'path':self.path, 'target':target, 'headers':{k:('[REDACTED]' if k.lower() in ('authorization','x-api-key') else v) for k,v in headers.items()}}
        try:
            if body:
                try: rec['request_json']=json.loads(body.decode('utf-8'))
                except Exception: rec['request_body']=body[:5000].decode('utf-8','replace')
            resp = requests.request(self.command, target, headers=headers, data=body, timeout=300, stream=False)
            rec['status_code']=resp.status_code
            rec['response_headers']={k:v for k,v in resp.headers.items()}
            rec['response_text']=resp.text[:12000]
            out = resp.content
            self.send_response(resp.status_code)
            for k,v in resp.headers.items():
                if k.lower() not in ('content-encoding','transfer-encoding','connection','content-length'):
                    self.send_header(k,v)
            self.send_header('Content-Length', str(len(out)))
            self.end_headers()
            self.wfile.write(out)
        except Exception as e:
            rec['proxy_error']=repr(e)
            out = json.dumps({'error':'proxy_error','detail':repr(e)}, ensure_ascii=False).encode('utf-8')
            self.send_response(502)
            self.send_header('Content-Type','application/json')
            self.send_header('Content-Length', str(len(out)))
            self.end_headers()
            self.wfile.write(out)
        finally:
            with LOG.open('a', encoding='utf-8') as f:
                f.write(json.dumps(rec, ensure_ascii=False, indent=2) + '\n---\n')
if __name__ == '__main__':
    port = int(os.environ.get('OPENCODE_PROXY_PORT','18082'))
    print(f'opencode proxy listening on 127.0.0.1:{port}', flush=True)
    ThreadingHTTPServer(('127.0.0.1', port), H).serve_forever()

