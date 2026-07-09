import pathlib, re, os
p=pathlib.Path(os.environ.get(
    'GATEWAYD_BIN',
    str(pathlib.Path(__file__).resolve().parent / 'bin' / 'gatewayd.exe')
))
b=p.read_bytes()
for pat in [b'compat', b'bridge', b'fallback', b'tools', b'reasoning', b'truncation', b'unsupported', b'exclude_user_agents']:
    print('\nPAT', pat.decode())
    n=0
    for m in re.finditer(pat, b, re.I):
        s=max(0,m.start()-160); e=min(len(b), m.end()+260)
        chunk=b[s:e]
        txt=''.join(chr(c) if 32<=c<127 else ' ' for c in chunk)
        print(txt[:500])
        n+=1
        if n>=3: break
