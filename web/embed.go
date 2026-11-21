package web

import "embed"

// Assets embeds the web dashboard static assets (HTML, CSS, JS).
//
//go:embed index.html styles.css app.js
var Assets embed.FS
