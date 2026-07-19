package render

// compatScript patches small web-platform gaps in gost-dom that
// mainstream SPAs rely on at startup. Each patch is defensive: native
// implementations are used when present (Response.text falls back only
// when the built-in binding throws its not-implemented error), so the
// script keeps working as gost-dom fills the gaps upstream.
//
// The script is prepended to the seed document. Content before the
// doctype is legal tag soup per the HTML parsing spec: the script
// element lands in the implicit head and runs before any page script.
const compatScript = `<script>(function () {
"use strict";

function readAllText(body) {
	var reader = body.body.getReader();
	var decoder = new TextDecoder();
	var out = "";
	function pump() {
		return reader.read().then(function (r) {
			if (r.done) { return out; }
			out += decoder.decode(r.value, { stream: true });
			return pump();
		});
	}
	return pump();
}

if (typeof Response !== "undefined") {
	var origText = Response.prototype.text;
	Response.prototype.text = function () {
		try { return origText.call(this); } catch (e) { return readAllText(this); }
	};
}

function memStorage() {
	var m = new Map();
	return {
		get length() { return m.size; },
		key: function (i) { var k = Array.from(m.keys())[i]; return k === undefined ? null : k; },
		getItem: function (k) { return m.has(String(k)) ? m.get(String(k)) : null; },
		setItem: function (k, v) { m.set(String(k), String(v)); },
		removeItem: function (k) { m.delete(String(k)); },
		clear: function () { m.clear(); }
	};
}

if (typeof window.localStorage === "undefined") { window.localStorage = memStorage(); }
if (typeof window.sessionStorage === "undefined") { window.sessionStorage = memStorage(); }

if (typeof window.matchMedia === "undefined") {
	window.matchMedia = function (q) {
		return {
			matches: false,
			media: String(q),
			onchange: null,
			addListener: function () {},
			removeListener: function () {},
			addEventListener: function () {},
			removeEventListener: function () {},
			dispatchEvent: function () { return false; }
		};
	};
}
})();</script>`

// withCompatScript prepends [compatScript] to the seed document.
func withCompatScript(body []byte) []byte {
	return append([]byte(compatScript), body...)
}
