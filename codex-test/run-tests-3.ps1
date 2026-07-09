# Phase-3: live SSRF reproduction
$ErrorActionPreference='Continue'
$root='D:\EliuaK_Csy\Working-Paper\My-Program\Ai-Model-Gateway'
$out="$root\codex-test\out"
$base='http://127.0.0.1:18080'
$log="$root\.gateway-runtime\logs\gatewayd.log"
New-Item $out -ItemType Directory -Force | Out-Null

$baseline=(Get-Content $log).Count
$baseline | Out-File "$out\gwlog3.baseline" -Encoding ascii

function Send-Req($name,$method,$path,$body){
  $url="$base$path"
  $sw=[Diagnostics.Stopwatch]::StartNew()
  $resp=@{name=$name;status=0;ms=0;len=0;body='';err=''}
  try{
    $hdr=@{'Content-Type'='application/json'}
    $r=Invoke-WebRequest $url -Method $method -Headers $hdr -Body $body -UseBasicParsing -TimeoutSec 35
    $resp.status=[int]$r.StatusCode
    $resp.body=$r.Content
  }catch{
    $resp.status=[int]$_.Exception.Response.StatusCode
    $resp.err=$_.Exception.Message
    try{ $resp.body=$_.Exception.Response.StatusCode.value__ }catch{}
    try{ $stream=$_.Exception.Response.GetResponseStream(); $sr=New-Object IO.StreamReader($stream); $resp.body=$sr.ReadToEnd() }catch{ $resp.body=$resp.err }
  }
  $sw.Stop(); $resp.ms=[int]$sw.ElapsedMilliseconds
  $resp.len=$resp.body.Length
  if($resp.body -and $resp.body.Length -gt 4000){ $resp.body=$resp.body.Substring(0,4000) }
  $resp.body | Out-File "$out\$name.body" -Encoding utf8 -NoNewline
  return $resp
}

$tests=@(
  @{name='ssrf1-chat-localhost';path='/v1/chat/completions';body='{"model":"ssrf-local-model","messages":[{"role":"user","content":"hi"}],"max_tokens":4}'}
  @{name='ssrf2-chat-loopback';path='/v1/chat/completions';body='{"model":"ssrf-loopback-model","messages":[{"role":"user","content":"hi"}],"max_tokens":4}'}
  @{name='ssrf3-chat-private';path='/v1/chat/completions';body='{"model":"ssrf-private-model","messages":[{"role":"user","content":"hi"}],"max_tokens":4}'}
  @{name='ssrf4-resp-localhost';path='/v1/responses';body='{"model":"ssrf-local-model","input":"hi","max_output_tokens":4}'}
  @{name='ssrf5-anth-localhost';path='/v1/messages';body='{"model":"ssrf-local-model","messages":[{"role":"user","content":"hi"}],"max_tokens":4}'}
)

">>> Phase-3 SSRF tests started: $(Get-Date -Format o)"
$table=@()
foreach($t in $tests){ Start-Sleep -Milliseconds 200; $r=Send-Req $t.name 'POST' $t.path $t.body; $table+=$r; "[$($r.name)] status=$($r.status) ms=$($r.ms) len=$($r.len) err=$($r.err)" }
"`n# Phase-3 SSRF run $(Get-Date -Format o)`n| name | status | ms | len |`n|---|---|---|---|"
foreach($r in $table){ "| $($r.name) | $($r.status) | $($r.ms) | $($r.len) |" }

$after=(Get-Content $log).Count
"`n===== gatewayd.log NEW lines (Phase-3) ====="
Get-Content $log | Select-Object -Skip $baseline -First ($after-$baseline)
"Phase-3 done."
