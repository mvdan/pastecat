# pastecat

A very simple and self-hosted pastebin service written in Go. Can use multiple
storage backends.

Designed to remove pastes after a certain period of time. If using a
persistent storage backend, pastes will be kept between restarts.

This software is what runs the [paste.cat](http://paste.cat) public service.

### Use

Set up an alias:

	$ alias pcat='curl -F "paste=<-" http://paste.cat'

Upload a new paste:

	$ echo foo | pcat
	http://paste.cat/a63d03b9

Fetch it:

	$ curl http://paste.cat/a63d03b9
	foo

### Build

	$ go build

### Run

##### Quick setup

	$ pastecat -u http://my.site -l :80

It will stay in the foreground and periodically print usage stats.

##### Options

* **-u** - URL of the site - *http://localhost:8080*
* **-l** - Host and port to listen to - *:8080*
* **-t** - Lifetime of the pastes - *24h*
* **-m** - Maximum number of pastes to store at once - *0*
* **-s** - Maximum size of pastes - *1M*
* **-M** - Maximum storage size to use at once - *1G*

Any of the options requiring quantities can take a zero value as infinity.

##### Storage backends

You may specify any of the following storage backends as arguments right after
the options.

Persistent:

* **fs** *[directory]* - filesystem structure *(default)*
* **mmap** *[directory]* - mmapped filesystem structure

Non-persistent:

* **mem** - standard in-memory map

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
