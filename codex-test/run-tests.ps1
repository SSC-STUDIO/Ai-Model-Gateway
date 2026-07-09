$ErrorActionPreference = 'Continue'
$base = 'http://127.0.0.1:18080'
$root = 'D:\EliuaK_Csy\Working-Paper\My-Program\Ai-Model-Gateway'
$gwlog = "$root\.gateway-runtime\logs\gatewayd.log"
$results = "$root\codex-test\out\results.md"
"# Ai-Model-Gateway live test run $(Get-Date -Format o)" | Out-File $results -Encoding utf8
function Read-ErrorBody($resp){
  if(-not $resp){ return '' }
  try{
    $s = $resp.GetResponseStream()
    if(-not $s){ return '' }
    $sr = New-Object System.IO.StreamReader($s)
    $body = $sr.ReadToEnd()
    return $body
  }catch{ return "[unreadable:$($_.Exception.Message)]" }
}
function TT($name,$url,$body,$timeout=90){
  $sw = [System.Diagnostics.Stopwatch]::StartNew()
  $line = ''
  try{
    $r = Invoke-WebRequest -Uri $url -Method POST -Body $body -ContentType 'application/json' -UseBasicParsing -TimeoutSec $timeout
    $line = "| $name | PASS | $($r.StatusCode) | $($sw.ElapsedMilliseconds)ms | $($r.Content.Length)B |"
    $r.Content.Substring(0,[Math]::Min(1800,$r.Content.Length)) | Out-File "$root\codex-test\out\$name.body" -Encoding utf8
  }catch{
    $resp = $_.Exception.Response
    $code = if($resp){ [int]$resp.StatusCode } else { 'ERR' }
    $eb = Read-ErrorBody $resp
    $line = "| $name | FAIL | $code | $($sw.ElapsedMilliseconds)ms | $($eb.Length)B |"
    if($eb){ $eb.Substring(0,[Math]::Min(1800,$eb.Length)) | Out-File "$root\codex-test\out\$name.body" -Encoding utf8 }
    else{ $_.Exception.Message | Out-File "$root\codex-test\out\$name.body" -Encoding utf8 }
  }
  Write-Host $line
  Add-Content $results $line
}
"$(Get-Date -Format HH:mm:ss) starting inference battery"
# T1 direct chat to opencode-deepseek deepseek-v4-flash
TT 't1-chat-deepseek' "$base/v1/chat/completions" '{"model":"deepseek-v4-flash","messages":[{"role":"user","content":"Say hi in one word"}],"max_tokens":16}'
# T2 direct chat to nowcoding gpt-5.5
TT 't2-chat-gpt5.5' "$base/v1/chat/completions" '{"model":"gpt-5.5","messages":[{"role":"user","content":"Reply with the single word: ok"}],"max_tokens":16}'
# T3 compat bridge: gpt-5.4-mini -> deepseek-v4-flash (non-stream)
TT 't3-bridge-gpt5.4-mini' "$base/v1/chat/completions" '{"model":"gpt-5.4-mini","messages":[{"role":"user","content":"Say hi in one word"}],"max_tokens":16}'
# T4 OpenAI Responses API for gpt-5.5 (responses->chat via nowcoding)
TT 't4-responses-gpt5.5' "$base/v1/responses" '{"model":"gpt-5.5","input":[{"role":"user","content":[{"type":"input_text","text":"Reply with ok"}]}],"max_output_tokens":16}'
# T5 OpenAI Responses API for gpt-5.4-mini (responses->chat + bridge -> deepseek)
TT 't5-responses-bridge' "$base/v1/responses" '{"model":"gpt-5.4-mini","input":[{"role":"user","content":[{"type":"input_text","text":"Reply with ok"}]}],"max_output_tokens":16}'
# T6 streaming chat deepseek with stream_options usage
(TT 't6-stream-deepseek' "$base/v1/chat/completions" '{"model":"deepseek-v4-flash","messages":[{"role":"user","content":"count 1 to 3"}],"max_tokens":16,"stream":true,"stream_options":{"include_usage":true}}')
# T7 streaming + bridge gpt-5.4-mini
(TT 't7-stream-bridge' "$base/v1/chat/completions" '{"model":"gpt-5.4-mini","messages":[{"role":"user","content":"count 1 to 3"}],"max_tokens":16,"stream":true,"stream_options":{"include_usage":true}}')
# T8 Anthropic Messages API
(TT 't8-anthropic-messages' "$base/v1/messages" '{"model":"deepseek-v4-flash","max_tokens":16,"messages":[{"role":"user","content":"Say hi"}]}')
# T9 chat with tools (deepseek) - tool passthrough
TT 't9-tools-deepseek' "$base/v1/chat/completions" '{"model":"deepseek-v4-flash","messages":[{"role":"user","content":"What is the weather in Tokyo? Use the tool."}],"max_tokens":48,"tools":[{"type":"function","function":{"name":"get_weather","description":"get weather","parameters":{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}}}]}'
# T10 response_format json object
TT 't10-response_format' "$base/v1/chat/completions" '{"model":"deepseek-v4-flash","messages":[{"role":"user","content":"Return a JSON object {ok true}"}],"max_tokens":24,"response_format":{"type":"json_object"}}'
"$(Get-Date -Format HH:mm:ss) battery done"
""  | Out-File "$root\codex-test\out\gwlog.delta" -Encoding utf8
"" | Out-File "$root\codex-test\out\gwlog.delta"
"`n===== gatewayd.log NEW lines (since baseline) =====" | Add-Content $results
Get-Content $gwlog | Select-Object -Skip 550 | Add-Content $results
Write-Host "===== gatewayd.log delta since baseline 550 ====="
Get-Content $gwlog | Select-Object -Skip 550
