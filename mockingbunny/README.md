# mockingbunny

A fully interactive, mux shell for Linux extended with common offsec functionality.
- **bunnyears** - listener++
- **bits** - remote shell payload

> The spirit bunny sweeps its tail. Erasing traces creates traces.

## Fully interactive

No need to upgrade your shell. Remote shell already has:
- PTY shell
- Control-characters, e.g.:
  - [tab] for auto-complete
  - [up] and [down] for history
  - [ctrl+c] kills the remote process instead of the local shell listener
  - etc.
- Dynamic window resizing
- Colors where applicable

## Multiplex (mux)

The network connection is multiplexed to enable more than one stream at a time on the same socket/port. It allows us to execute commands on the background and other concurrent workflows.
