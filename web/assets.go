// Package web contains the dashboard's dependency-free browser application.
package web

import "embed"

// FS includes only public browser assets.
//go:embed index.html app.js i18n.js styles.css locales/*.js
var FS embed.FS
