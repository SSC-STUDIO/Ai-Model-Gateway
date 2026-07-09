$ErrorActionPreference = 'Continue'
$base = 'http://127.0.0.1:18080'
$root = 'D:\EliuaK_Csy\Working-Paper\My-Program\Ai-Model-Gateway'
$out  = "$root\codex-test\out"
$rep  = "$out\results-2.md"
"# Phase-2 live test run $(Get-Date -Format o)" | Out-File $rep -Encoding utf8

# Save full body (no truncation), return status code + timing + size.
function Send($name,$method,$url,$body,$headers,$timeout=60){
  $sw = [System.Diagnostics.Stopwatch]::StartNew()
  $code = '?'; $len = 0; $errLine = ''
  try{
    $req = [System.Net.HttpWebRequest]::Create($url)
    $req.Method = $method
    $req.Timeout = $timeout*1000
    $req.ReadWriteTimeout = $timeout*1000
    $req.ContentType = 'application/json'
    if($headers){ $headers.GetEnumerator() | ForEach-Object { $req.Headers.Add($_.Key,$_.Value) } }
    if($body){
      $bytes = [System.Text.Encoding]::UTF8.GetBytes($body)
      $req.ContentLength = $bytes.Length
      $s = $req.GetRequestStream(); $s.Write($bytes,0,$bytes.Length); $s.Close()
    }
    $resp = $null
    try{ $resp = $req.GetResponse() } catch [System.Net.WebException]{
      if($_.Exception.Response){ $resp = $_.Exception.Response } else { throw } }
    $code = [int]$resp.StatusCode
    $rs = $resp.GetResponseStream()
    $sr = New-Object System.IO.StreamReader($rs)
    $content = $sr.ReadToEnd(); $sr.Close(); $rs.Close(); $resp.Close()
    $len = $content.Length
    $content | Out-File "$out\$name.body" -Encoding utf8
  }catch{
    $errLine = $_.Exception.Message
    $code = 'ERR'
    $errLine | Out-File "$out\$name.body" -Encoding utf8
  }
  $ms = $sw.ElapsedMilliseconds
  $line = "| $name | $code | ${ms}ms | ${len}B | $($errLine) |"
  Write-Host $line
  Add-Content $rep $line
}

"`n## Streaming (full capture, include_usage=true)`n"
Send 's1-stream-deepseek-usage' 'POST' "$base/v1/chat/completions" '{"model":"deepseek-v4-flash","messages":[{"role":"user","content":"count 1 to 3"}],"max_tokens":16,"stream":true,"stream_options":{"include_usage":true}}' $null 60
Send 's2-stream-bridge-usage'  'POST' "$base/v1/chat/completions" '{"model":"gpt-5.4-mini","messages":[{"role":"user","content":"count 1 to 3"}],"max_tokens":16,"stream":true,"stream_options":{"include_usage":true}}' $null 60
Send 's3-stream-no-usage'       'POST' "$base/v1/chat/completions" '{"model":"deepseek-v4-flash","messages":[{"role":"user","content":"count 1 to 3"}],"max_tokens":16,"stream":true,"stream_options":{"include_usage":false}}' $null 60

"`n## Error-format compliance`n"
Send 'e1-unknown-model'        'POST' "$base/v1/chat/completions" '{"model":"does-not-exist","messages":[{"role":"user","content":"hi"}],"max_tokens":8}' $null 30
Send 'e2-invalid-json'         'POST' "$base/v1/chat/completions" '{this is not json' $null 30
Send 'e3-empty-body'           'POST' "$base/v1/chat/completions" '' $null 30
Send 'e4-missing-messages'     'POST' "$base/v1/chat/completions" '{"model":"deepseek-v4-flash","max_tokens":8}' $null 30
Send 'e5-missing-model'        'POST' "$base/v1/chat/completions" '{"messages":[{"role":"user","content":"hi"}],"max_tokens":8}' $null 30
Send 'e6-bad-model-responses'   'POST' "$base/v1/responses" '{"model":"ghost-model","input":[{"role":"user","content":[{"type":"input_text","text":"hi"}]}],"max_output_tokens":8}' $null 30
Send 'e7-anthropic-bad-model'  'POST' "$base/v1/messages" '{"model":"ghost-model","max_tokens":8,"messages":[{"role":"user","content":"hi"}]}' $null 30
Send 'e8-tool-bad-schema'      'POST' "$base/v1/chat/completions" '{"model":"deepseek-v4-flash","messages":[{"role":"user","content":"hi"}],"max_tokens":8,"tools":[{"type":"function","function":{"name":"x"}}]}' $null 30

"`n## Data-plane auth probes`n"
$noauth = @{}
Send 'a1-models-no-auth'    'GET' "$base/v1/models" $null $noauth 20
Send 'a2-models-dummy-auth' 'GET' "$base/v1/models" $null @{Authorization='Bearer dummy'} 20
Send 'a3-chat-no-auth'      'POST' "$base/v1/chat/completions" '{"model":"deepseek-v4-flash","messages":[{"role":"user","content":"hi"}],"max_tokens":4}' $noauth 30
Send 'a4-chat-empty-bearer' 'POST' "$base/v1/chat/completions" '{"model":"deepseek-v4-flash","messages":[{"role":"user","content":"hi"}],"max_tokens":4}' @{Authorization='Bearer '} 30

"`n## GET endpoints`n"
Send 'g1-health'    'GET' "$base/health" $null $noauth 10
Send 'g2-ready'     'GET' "$base/ready"  $null $noauth 10
Send 'g3-v1-models'  'GET' "$base/v1/models" $null @{Authorization='Bearer dummy'} 10
Send 'g4-root'      'GET' "$base/" $null $noauth 10

"`n## Done.`n"
Write-Host "Saved to $rep"
