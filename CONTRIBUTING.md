# Contributing to ALC's Forgejo Runner fork

This fork accepts AI-assisted code, tests, documentation and discussion.
Contributors are responsible for understanding their changes, checking their
accuracy, testing the behavior they change and following applicable licenses.
Contributions receive the same review regardless of the tools used to write them.

Open issues and pull requests on the
[canonical Forgejo repository](https://git.alc.xyz/alcxyz/forgejo-runner).
Discuss changes that alter runner behavior or compatibility before implementing
them. Keep each pull request focused and explain the problem, the change and
how it was verified. Branch from `dev` and target `dev`; promotions to `main`
are reviewed separately and merged fast-forward only.

Keep existing copyright and license notices. Contributions must be compatible
with this repository's [GPL-3.0-or-later license](LICENSE) and applicable file
licenses. This fork's contribution policy applies here; upstream maintains its
own contribution policy.

For runner changes, run:

```sh
gofmt -w <changed Go files>
go test ./internal/app/poll ./act/runner -run 'TestPause|TestPollerPause|TestSDNotify|TestRemoveDockerActionContainer' -count=1 -args -features=-
go test -short ./... -args -features=-
make build
```

The disabled feature tests require separate Docker/Podman or LXC integration
validation when a change affects those paths. Include the test commands,
results and any untested behavior in the pull request.
