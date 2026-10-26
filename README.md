# LNF — Love Nature Frames

LNF is a Wayland layer-shell panel framework written in pure Go. It doesn't
replace your window manager — it draws the bar and the widgets on top of one
(sway/swayfx, niri, Hyprland).

![panel](docs/panel.png)

What about resource usage? I tested it on my own hardware:

- ~23 MB PSS (26.5 MB RSS)
- 0.3% of one core, averaged over two hours
- 19 threads, 25 seconds of CPU time in that time

Under the hood: no cgo, no GL, no JS runtime.

Don't expect pure bliss: the config is Go too, so every change means a
recompile. That's the trade-off for the low footprint.

## Build & run

Requires Go 1.27+ and a compositor with `wlr-layer-shell`.

```sh
go build -o lnf .
./lnf
```

The panel is defined in `panel.go` (`panelConfig`) — edit it and rebuild.
Full reference: [CONFIG.md](CONFIG.md).

Render the scenes to PNG without a compositor:

```sh
LNF_DUMP=out.png ./lnf
```

## License

CC BY-NC-SA 4.0. Fork it, credit the author, keep the same license, don't
sell it. Full terms in [LICENSE](LICENSE).

Copyright (c) 2026 baduga2007-netizen

---

Be good to each other, enjoy it, and please respect the license.

And of course: make friends with every distro, respect systemd, hate Ubuntu.
