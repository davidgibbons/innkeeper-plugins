# Golden cards

- `v3-mirelle.json`: the standard CCv3 template from the owner's sillytavern-skills repo.
- `v3-seraphina.png`: SillyTavern's default sample card, `default/content/default_Seraphina.png` in github.com/SillyTavern/SillyTavern. It has V2 in `chara` and V3 in `ccv3`.
- `v2-seraphina.png`: the same file with its `ccv3` chunk removed.
- `v2-seraphina.json`: that file's V2 card as JSON.
- `v3-mirelle.charx`: `v3-mirelle.json` as CharX, with a main icon asset holding a 2x3 PNG.

Generate the `.want.json` files with `go test ./card -run TestGolden -update`. Review them; don't hand-edit.
- `eldoria.lorebook.json`: SillyTavern's sample world info, `default/content/Eldoria.json` in the same repo. `TestGolden` skips `*.lorebook.json`; `TestLorebookGolden` reads them.
