# mockingbunny

> The spirit bunny sweeps its tail. Erasing traces creates traces.

A fully interactive, mux shell for Linux extended with common offsec functionality.
- **bunnyears** - listener++
- **bits** - remote shell payload

## Features

### Fully interactive

No need to upgrade your shell. Remote shell already has:
- PTY shell
- Raw input for control-characters, e.g.:
  - [tab] for auto-complete
  - [up] and [down] for history
  - [ctrl+c] kills the remote process instead of the local shell listener
  - etc.
- Colors the shell where applicable
- Dynamic window resizing
  - Resizing the local terminal also resizes the remote TTY

### Multiplex (mux)

The network connection is multiplexed to enable more than one stream at a time on the same socket/port. 

It allows programmatic execution of commands in the background and other concurrent workflows.

## TODO

- [ ]  Send and listen for ack before starting yamux to be able to receive errors coming from proxies

- [ ]  Multiplex connection with rand num to identify stream

- [ ]  Ping back after timeout idle

- [ ]  Background cmds

- [ ]  Expose option for plain TCP socket on flags — dumb TTY instead of FIT (Fully-Interactive Terminal)

- [ ]  Bind TCP shell

- [ ]  Change direction of stream creation — [NAT traversal](https://en.wikipedia.org/wiki/NAT_traversal)

- [ ]  PrivEsc

- [ ]  File transfer

    - [ ]  HTTP

    - [ ]  TCP

    - [ ]  UDP
