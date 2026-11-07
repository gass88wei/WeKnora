This directory holds the Vite build output (frontend/dist).

It is committed only so `//go:embed dist` in ../embed.go always compiles
(e.g. in lint jobs or bindings generation that never run the frontend build).
Real builds replace these files: the build-desktop-app job downloads the
frontend-dist artifact here, which contains index.html + assets/.
