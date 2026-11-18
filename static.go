package main

import _ "embed"

//go:embed static/bdag-community-logo.svg
var brandLogoSVG []byte

//go:embed static/htmx-2.0.4.min.js
var htmxJS []byte
