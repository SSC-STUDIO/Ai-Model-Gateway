## Summary

- What changed?
- Why was this needed?

## Checklist

- [ ] I ran `go test ./...`
- [ ] I ran `npm --prefix web/admin test` and `npm --prefix web/admin run build` when the admin UI or shared frontend utilities changed
- [ ] I ran `go build` for impacted commands when packaging or release behavior changed
- [ ] I did not commit real secrets, local configs, telemetry DBs, or logs
- [ ] I documented any config or behavior changes
- [ ] I added screenshots for admin UI changes, if applicable
- [ ] I checked `git status --short --ignored` for generated files before staging

## Risk

- Protocol compatibility impact:
- Routing / retry impact:
- Monitoring / pricing / ops impact:

## Notes

- Anything reviewers should verify manually?
- Local validation summary:
