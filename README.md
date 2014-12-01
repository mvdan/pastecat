# Pastecat

A very simple and self-hosted pastebin service written in Go. Stores
plaintext pastes in a filesystem hierarchy.

Designed to remove pastes after a certain period of time. Upon restart, file
modification times will be used to recover the removal time of each paste.

This software is what runs [paste.cat](http://paste.cat) for public use.

### Build

	$ go build

### Run

##### Quick setup

	$ pastecat fs -u http://my.site -l :80

It will stay in the foreground and periodically print usage stats.

##### Options

* **-u** - URL of the site - *http://localhost:8080*
* **-l** - Host and port to listen to - *:8080*
* **-t** - Lifetime of the pastes - *24h*
* **-s** - Maximum size of pastes - *1M*
* **-m** - Maximum number of pastes to store at once - *0*
* **-M** - Maximum storage size to use at once - *1G*

##### Storage backends

You may specify any of the following storage backends as arguments as shown in
the quick setup example.

Persistent:

* **fs** *[dir=pastes]* - Use a filesystem directory structure - *(default)*

Non-persistent:

* **mem** - Use a standard in-memory map without persistence

### Use

Set up an alias for easy usage:

	$ alias pcat='curl -F "paste=<-" http://paste.cat'

Upload a new paste via standard input:

	$ echo foo | pcat
	http://paste.cat/a63d03b9

Fetch it:

	$ curl http://paste.cat/a63d03b9
	foo

### What it doesn't do

##### Compression

Should be handled at a lower level. Filesystems like Btrfs already support
compression.

##### Content-Types (mimetypes)

A pastebin service is, by definition, aimed at plaintext only. All content is
stored and served in UTF-8.

##### Shiny web interface

You can build one on top of pastecat, using it as the backend. The builtin web
interface is only a fallback for those cases where using the command line
interface is not an option.

This includes syntax highlighting, which can be done either via CSS on a web
interface or via piping plaintext to programs like highlight.

##### HTTPS

Even though security could be accomplished over plain HTTP by using tools like
GnuPG on the client side, for privacy reasons you might want to support HTTPS
as well.

In such cases, running pastecat behind a reverse proxy like Nginx is the best
option. HTTP servers should have lots of features including TLS support.
