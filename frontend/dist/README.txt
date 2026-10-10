This directory holds the Vite build output (frontend/dist).

It is committed only so `//go:embed dist` in ../embed.go always compiles
(e.g. in lint jobs that never run the frontend build). Real builds replace
these files: `wails build` runs `vite build` first, which empties this
directory and writes index.html + assets/.
