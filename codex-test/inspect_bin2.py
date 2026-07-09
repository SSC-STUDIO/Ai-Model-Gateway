import re
path=r'D:\EliuaK_Csy\Working-Paper\My-Program\Ai-Model-Gateway\bin\gatewayd.exe'
b=open(path,'rb').read()
pats=[b'access not allowed',b'localhost access',b'private IP access',b'no provider available',b'uptime',b'includeUsage',b'include_usage',b'[DONE]',b'infinite',b'max_retries',b'reasoning_content',b'attempt=%d/%d',b'GatewayStatusResponse']
for pat in pats:
    hits=[m.start() for m in re.finditer(re.escape(pat),b)]
    print('\n### %-22s hits=%d' % (pat.decode(errors='replace'),len(hits)))
    for s in hits[:2]:
        chunk=b[max(0,s-80):s+120]
        txt=''.join(chr(c) if 32<=c<127 else '.' for c in chunk)
        print('  |%s' % txt[:210])
