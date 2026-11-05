A collection of inherited Forgejo Actions examples. These examples are not
covered by this fork's unit-test CI; validate them in a disposable environment.

Other examples can be found [in the documentation](https://forgejo.org/docs/next/user/actions/).

| Section                     | Description                                                                                                                                                                                                                                                   |
|-----------------------------|--------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| [`docker-build-push-action`](docker-build-push-action)              | minimal Docker build context retained from the former build-and-push workflow                     |
| [`docker`](docker)              | using the host docker server by mounting the socket                     |
| [`LXC systemd`](lxc-systemd)              | systemd unit managing LXC containers dedicated to a single runner                     |
| [`docker-compose`](docker-compose) | all in one docker-compose with the Forgejo server, the runner and docker in docker  |
| [`kubernetes`](kubernetes)     |  a sample deployment for the Forgejo runner |
| [`on-demand`](on-demand)     |  create runners in response to pending Forgejo Action jobs |
