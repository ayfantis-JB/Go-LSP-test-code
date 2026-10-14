# Go by Example

All 85 examples from [Go by Example](https://gobyexample.com)
(source: https://github.com/mmcgrana/gobyexample, `examples/`, commit `28c81a7a0eb874a1c062ae28d19b88effd24620f`, 2026-09-07).
Copied verbatim: only the `.go` files, plus the data files that `embed-directive` embeds.
The upstream `.sh` transcripts and `.hash` files are not included.

Go by Example is copyright Mark McGranaghan and licensed under a
[Creative Commons Attribution 3.0 Unported License](http://creativecommons.org/licenses/by/3.0/).

## Notes for this lab

* Upstream targets Go 1.27. Everything builds with Go 1.26.5. The one exception is `json`,
  which imports `encoding/json/v2` and needs `GOEXPERIMENT=jsonv2` on 1.26. The lab sets
  that for its own Go commands and in the containers. On the host, the IDE shows an error
  in `json/json.go` unless the experiment is set, which is a useful LSP check in itself.
* `go run ./tools/lab run-examples` runs every example. The table in
  `tools/internal/lab/examples.go` supplies arguments, stdin, expected exit codes and setup
  for the ones that need them, and interrupts the servers after a few seconds.
