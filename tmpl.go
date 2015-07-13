// Copyright (c) 2014-2015, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package main

import "html/template"

var tmpl *template.Template

func loadTemplates() {
	for name, s := range templates {
		var t *template.Template
		if tmpl == nil {
			tmpl = template.New(name)
		}
		if name == tmpl.Name() {
			t = tmpl
		} else {
			t = tmpl.New(name)
		}
		if _, err := t.Parse(s); err != nil {
			panic("could not load templates")
		}
	}
}

var templates = map[string]string{
	"/": `<html>
<body style="text-align:center">
<pre style="display:inline-block;text-align:left;margin:2em 2em 2em 0">
Set up an alias:

    $ alias pcat='curl -F "{{.FieldName}}=&lt;-" {{.SiteURL}}'

Upload a new paste:

    $ echo foo | pcat
    {{.SiteURL}}/a63d03b9

Fetch it:

    $ curl {{.SiteURL}}/a63d03b9
    foo

You can also use the <a href="form">web form</a>.
{{if gt .MaxSize 0.0}}
The maximum size per paste is {{.MaxSize}}.
{{end}}{{if gt .LifeTime 0}}
Each paste will be deleted after {{.LifeTime}}.
{{end}}
<a href="http://github.com/mvdan/pastecat">github.com/mvdan/pastecat</a>
</pre>
</body>
</html>
`,
	"/form": `<html>
<body style="text-align:center">
<div style="inline-block">
	<form action="{{.SiteURL}}" method="post" enctype="multipart/form-data">
		<textarea cols=80 rows=24 name="{{.FieldName}}"></textarea>
		<br/>
		<button type="submit">Paste text</button>
	</form>
	<br/>
	<form action="{{.SiteURL}}" method="post" enctype="multipart/form-data">
		<input type="file" name="{{.FieldName}}"></input>
		<button type="submit">Paste file</button>
	</form>
</div>
</body>
</html>
`,
}
