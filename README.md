# pastecat

A simple and self-hosted pastebin service written in Go. Can use a variety of
storage backends.

Designed to optionally remove pastes after a certain period of time. If using
a persistent storage backend, pastes will be kept between runs.

### Use

Set up an alias:

	$ alias pcat='curl -F "paste=<-" http://my.site'

Upload a new paste:

	$ echo foo | pcat
	http://my.site/a63d03b9

Fetch it:

	$ curl http://my.site/a63d03b9
	foo

Doing a `POST` on `/redirect` will send you directly to the paste instead of
returning its url.

### Run

##### Quick setup

	$ pastecat -u http://my.site -l :80

##### Options

* **-u, --url** - URL of the site - *http://localhost:8080*
* **-l, --listen** - Host and port to listen to - *:8080*
* **-t, --lifetime** - Lifetime of the pastes - *24h*
* **-T, --timeout** - Timeout of HTTP requests - *5s*
* **-m, --max-number** - Maximum number of pastes to store at once - *0*
* **-s, --max-size** - Maximum size of pastes - *1M*
* **-M, --max-storage** - Maximum storage size to use at once - *1G*

Any of the options requiring quantities can take a zero value as infinity.

##### Storage backends

* **fs** *[directory]* - filesystem structure *(default)*
* **fs-mmap** *[directory]* - mmapped filesystem structure *(requires mmap)*
* **mem** - standard in-memory map *(non-persistent)*

Note that options must go first.

### What it doesn't do

##### Storage compression

Should be handled at a lower level. Filesystems like Btrfs already support
compression.

##### Content-Types (mimetypes)

A pastebin service is, by definition, aimed at plaintext only. All content is
stored and served in UTF-8.

##### Shiny web interface

You can build one on top with pastecat as the backend. The builtin web
interface is only a fallback for when the command line is not available.

This includes syntax highlighting of any kind.

##### HTTPS

Even though you could encrypt pastes with tools like GnuPG, for privacy
reasons you might want to support HTTPS too.

In such cases, you can run pastecat behind a reverse proxy like Nginx.

##### HTTP compression

Like HTTPS, you can use software like Nginx to add compression on top of
pastecat.
