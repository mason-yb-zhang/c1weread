# Third-Party Notices

## Code

### reader-lab (C1-Slim book reader / 墨页实验室)

- Source: `D:\dev\c1-slim\repos\reader-lab`
- License: GNU General Public License v3.0 (GPL-3.0)
- Usage in c1weread:
  - Font engine (`cmd/c1weread/fonts.go`, `cmd/c1weread/font_host.go`, `cmd/c1weread/font_linux.go`) adapted from `reader-lab/App/book-reader/lab_settings.go`, `lab_font_host.go`, `lab_font_linux.go`.
  - Pagination (`cmd/c1weread/pagination.go`) adapted from `reader-lab/App/book-reader/pager.go` and `chapter_pages.go`.
  - Settings view interaction inspired by `reader-lab/App/book-reader/app.go` and `lab_settings.go`.
  - `c1device` package replaced with `reader-lab/App/c1device`, which extends the upstream C1ancher `c1device` with `NewScaledBitmapFace` and `NewPixel12Face`.

## Fonts

### C1 Package Bitmap (Unifont subset)

- Source: GNU Unifont 16.0.04
- License: SIL Open Font License 1.1 / GPL v2+ with font embedding exception
- File: embedded `cmd/c1weread/assets/pkg-font.bin`
- Full upstream notices: `assets/font-LICENSE.txt` and `payload/assets/fonts/pkg-font-LICENSE.txt`

### Fusion Pixel 12px

- Source: https://github.com/TakWolf/fusion-pixel-font
- License: SIL Open Font License 1.1
- File: `payload/assets/fonts/fusion12.bin`
- License file: `payload/assets/fonts/fusion12-licenses/OFL.txt`
- Derived from cubic-11 and ark-pixel; see `payload/assets/fonts/fusion12-licenses/LICENSES/`

### LXGW WenKai Lite / 霞鹜文楷

- Source: https://github.com/lxgw/LxgwWenKai
- License: SIL Open Font License 1.1
- File: `payload/assets/fonts/LXGWWenKaiLite-Regular.ttf`
- License file: `payload/assets/fonts/OFL-LXGW.txt`
