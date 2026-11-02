# Third-party notices

deck embeds the following third-party micro plugins and colorscheme in
`internal/editor/runtime/` and installs them into `~/.config/deck/micro`.
`scripts/vendor-micro-plugins.sh` re-fetches the plugins at the commits below.
Each plugin directory keeps its upstream `LICENSE`.

| Component | Source | Commit | Licence |
|---|---|---|---|
| mlsp | https://github.com/Andriamanitra/mlsp | `91261a0926c9e95d059cf5854a0c2e8d4e7d4051` | MIT |
| filemanager | https://github.com/micro-editor/updated-plugins (`filemanager-plugin/`) | `216ec3adaf3adec78665614402ece56cf60ae713` | MIT |
| MicroOmni | https://github.com/Neko-Box-Coder/MicroOmni | `f770333935cf47318300309ffc0b2867a190e48e` | Unlicense |
| editorconfig | https://github.com/10sr/editorconfig-micro | `892aa42b50e6f57a5065b6f4df8c4e0a3913c9ee` | MIT |
| Dracula for micro | https://github.com/dracula/micro | vendored file `runtime/colorschemes/dracula.micro` | MIT |

`runtime/plug/deck/` and `runtime/plug/mlsp/z_deck.lua.tmpl` are part of deck.
