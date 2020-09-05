# portpoker

Simple TCP/Connect port scanner.

## TODO

- [ ]  parse args, especially ports (e.g. -p80,443,1024-2000,3389 and -p-)

- [ ]  retry on `i/o: too many files open` error

    - [ ]  regulate number of threads

    - [ ]  max retries

- [ ]  Return open/closed/filtered

- [ ]  Incremental, slower scans

    - Only for "filtered" and unscanned ports so far

    - Skip open and closed ports
