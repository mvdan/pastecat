/* Copyright (c) 2014-2015, Daniel Martí <mvdan@mvdan.cc> */
/* See LICENSE for licensing information */

package main

import (
	"html/template"
)

func loadTemplates() *template.Template {
	var t *template.Template
	for name, s := range templates {
		var tmpl *template.Template
		if t == nil {
			t = template.New(name)
		}
		if name == t.Name() {
			tmpl = t
		} else {
			tmpl = t.New(name)
		}
		if _, err := tmpl.Parse(s); err != nil {
			panic("could not load templates")
		}
	}
	return t
}

var templates = map[string]string{
	"/": `<html>
<body>
<pre>
Set up an alias:

    $ alias pcat='curl -F "{{.FieldName}}=&lt;-" {{.SiteURL}}'

Upload a new paste:

    $ echo foo | pcat
    {{.SiteURL}}/a63d03b9

Fetch it:

    $ curl {{.SiteURL}}/a63d03b9
    foo

You can also use the <a href="form">web form</a>.

{{.LimitDesc}}<a href="http://github.com/mvdan/pastecat">github.com/mvdan/pastecat</a>
</pre>
</body>
</html>
`,
	"/form": `<html>
<body>
	<form action="{{.SiteURL}}" method="post" enctype="multipart/form-data">
		<textarea cols=80 rows=24 name="{{.FieldName}}"></textarea>
		<br/>
		<button type="submit">Paste text</button>
	</form>
	<br/>
	<form action="{{.SiteURL}}" method="post" enctype="multipart/form-data">
		<input type="file" name="{{.FieldName}}"></input>
		<br/>
		<button type="submit">Paste file</button>
	</form>
	<p>{{.LimitDesc}}</p>
</body>
</html>
`,
}
