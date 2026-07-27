# Third-party licenses

`ike` is distributed under the [MIT License](LICENSE). A compiled `ike` binary
statically links the Go modules listed below, and this file provides the
attribution their licenses require — in particular the Apache-2.0 ones, whose
attribution obligation attaches when binaries are distributed rather than only
source.

Every dependency is under a permissive license (MIT, BSD-3-Clause, or
Apache-2.0). None is copyleft, and none imposes conditions on `ike` beyond
preserving these notices.

The full license text for each module ships inside its module cache directory
and can be read with:

```sh
go list -m -f '{{.Dir}}' <module-path>   # then open its LICENSE file
```

To see exactly which modules and versions went into a given binary:

```sh
go version -m $(command -v ike)
```

## Modules

| Module | License | Copyright |
| --- | --- | --- |
| `charm.land/bubbles/v2` | MIT | Copyright (c) 2020-2026 Charmbracelet, Inc. |
| `charm.land/bubbletea/v2` | MIT | Copyright (c) 2020-2026 Charmbracelet, Inc. |
| `charm.land/lipgloss/v2` | MIT | Copyright (c) 2021-2026 Charmbracelet, Inc. |
| `github.com/atotto/clipboard` | BSD-3-Clause | Copyright (c) 2013 Ato Araki. All rights reserved. |
| `github.com/charmbracelet/colorprofile` | MIT | Copyright (c) 2020-2024 Charmbracelet, Inc. |
| `github.com/charmbracelet/ultraviolet` | MIT | Copyright (c) 2025 Charmbracelet, Inc. |
| `github.com/charmbracelet/x/ansi` | MIT | Copyright (c) 2023 Charmbracelet, Inc. |
| `github.com/charmbracelet/x/term` | MIT | Copyright (c) 2023 Charmbracelet, Inc. |
| `github.com/charmbracelet/x/termios` | MIT | Copyright (c) 2023 Charmbracelet, Inc. |
| `github.com/charmbracelet/x/windows` | MIT | Copyright (c) 2023 Charmbracelet, Inc. |
| `github.com/clipperhouse/displaywidth` | MIT | Copyright (c) 2025 Matt Sherman |
| `github.com/clipperhouse/uax29/v2` | MIT | Copyright (c) 2020 Matt Sherman |
| `github.com/gofrs/flock` | BSD-3-Clause | Copyright (c) 2018-2025, The Gofrs; Copyright (c) 2015-2020, Tim Heckman |
| `github.com/google/jsonschema-go` | MIT | Copyright (c) 2025 JSON Schema Go Project Authors |
| `github.com/lucasb-eyer/go-colorful` | MIT | Copyright (c) 2013 Lucas Beyer |
| `github.com/mattn/go-runewidth` | MIT | Copyright (c) 2016 Yasuhiro Matsumoto |
| `github.com/modelcontextprotocol/go-sdk` | Apache-2.0 / MIT (see note) | Copyright (c) the MCP project authors |
| `github.com/muesli/cancelreader` | MIT | Copyright (c) 2022 Erik Geiser and Christian Muehlhaeuser |
| `github.com/rivo/uniseg` | MIT | Copyright (c) 2019 Oliver Kuederle |
| `github.com/segmentio/asm` | MIT | Copyright (c) 2021 Segment |
| `github.com/segmentio/encoding` | MIT | Copyright (c) 2019 Segment.io, Inc. |
| `github.com/spf13/cobra` | Apache-2.0 | Copyright (c) the Cobra authors |
| `github.com/spf13/pflag` | BSD-3-Clause | Copyright (c) 2012 Alex Ogier. All rights reserved.; Copyright (c) 2012 The Go Authors. All rights reserved. |
| `github.com/xo/terminfo` | MIT | Copyright (c) 2016 Anmol Sethi |
| `github.com/yosida95/uritemplate/v3` | BSD-3-Clause | Copyright (C) 2016, Kohei YOSHIDA. All rights reserved. |
| `golang.org/x/oauth2` | BSD-3-Clause | Copyright 2009 The Go Authors. |
| `golang.org/x/sync` | BSD-3-Clause | Copyright 2009 The Go Authors. |
| `golang.org/x/sys` | BSD-3-Clause | Copyright 2009 The Go Authors. |

`github.com/inconshreveable/mousetrap` (Apache-2.0, Copyright (c) 2022 Alan
Shreve) is a `cobra` dependency that is only linked on Windows.

## Note on the MCP SDK

`github.com/modelcontextprotocol/go-sdk` is mid-transition from MIT to
Apache-2.0. Its `LICENSE` states that new contributions and those with
relicensing consent are Apache-2.0, while contributions from authors who have
not consented remain MIT. Both are permissive and neither affects how `ike`
may be used or redistributed. `ike` pins this SDK to a v1.x stable release.

## Go standard library

`ike` also links the Go standard library and runtime, which are BSD-3-Clause,
Copyright (c) 2009 The Go Authors. See
<https://go.dev/LICENSE>.

## Logo

The logo in `assets/logo.svg` is based on Dwight D. Eisenhower's official White
House portrait — a work of the U.S. federal government, in the public domain.
See the `README.md` note for provenance.
