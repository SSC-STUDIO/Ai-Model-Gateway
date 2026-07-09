# Ai-Model-Gateway live test run 2026-07-06T22:26:45.8288008+08:00
| t1-chat-deepseek | PASS | 200 | 12883ms | 619B |
| t2-chat-gpt5.5 | PASS | 200 | 2832ms | 345B |
| t3-bridge-gpt5.4-mini | PASS | 200 | 1311ms | 603B |
| t4-responses-gpt5.5 | PASS | 200 | 8611ms | 394B |
| t5-responses-bridge | PASS | 200 | 1416ms | 367B |
| t6-stream-deepseek | PASS | 200 | 1360ms | 6161B |
| t7-stream-bridge | PASS | 200 | 1298ms | 6164B |
| t8-anthropic-messages | PASS | 200 | 1330ms | 271B |
| t9-tools-deepseek | PASS | 200 | 1690ms | 813B |
| t10-response_format | PASS | 200 | 1518ms | 665B |

===== gatewayd.log NEW lines (since baseline) =====
2026/07/06 22:26:45 [gatewayd] request_id=f5cb98e2-c273-488e-bc46-de135b0669b7 model=deepseek-v4-flash upstream_model=deepseek-v4-flash provider=opencode-deepseek attempt=1/0
2026/07/06 22:26:58 [gatewayd] request_id=09261271-19ba-4201-b46d-3bc8d24e99f6 model=gpt-5.5 upstream_model=gpt-5.5 provider=nowcoding attempt=1/0
2026/07/06 22:27:01 [gatewayd] request_id=df8ad19b-f38a-4862-bb4e-1c8b7d9dfbdd model=gpt-5.4-mini upstream_model=deepseek-v4-flash provider=opencode-deepseek attempt=1/0
2026/07/06 22:27:02 [gatewayd] request_id=fc87a000-f811-47ee-9dde-a3782525a2d9 model=gpt-5.5 upstream_model=gpt-5.5 provider=nowcoding attempt=1/0
2026/07/06 22:27:11 [gatewayd] request_id=72563295-3f1e-4427-b917-61556ceefd62 model=gpt-5.4-mini upstream_model=deepseek-v4-flash provider=opencode-deepseek attempt=1/0
2026/07/06 22:27:12 [gatewayd] request_id=74564dfb-4c73-48e6-8586-83edc7e3b745 model=deepseek-v4-flash upstream_model=deepseek-v4-flash provider=opencode-deepseek attempt=1/0
2026/07/06 22:27:14 [gatewayd] request_id=9f9acab0-27b1-440e-85ec-fbc1fa4679f9 model=gpt-5.4-mini upstream_model=deepseek-v4-flash provider=opencode-deepseek attempt=1/0
2026/07/06 22:27:15 [gatewayd] request_id=361e404d-34d1-40bd-b9b1-6d95e391224c model=deepseek-v4-flash upstream_model=deepseek-v4-flash provider=opencode-deepseek attempt=1/0
2026/07/06 22:27:16 [gatewayd] request_id=3960e401-de14-4224-95ab-a2e67ea9914b model=deepseek-v4-flash upstream_model=deepseek-v4-flash provider=opencode-deepseek attempt=1/0
2026/07/06 22:27:18 [gatewayd] request_id=73b400c4-50d4-4b28-b701-2924d05524c8 model=deepseek-v4-flash upstream_model=deepseek-v4-flash provider=opencode-deepseek attempt=1/0
