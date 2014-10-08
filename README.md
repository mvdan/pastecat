## Pastecat

A very simple and self-hosted pastebin service written in Go. Stores
zlib-compressed pastes in a filesystem hierarchy.

Designed to remove pastes after a certain period of time. Upon restart, file
modification times will be used to recover the removal time of each paste.

This software is what runs [paste.cat](http://paste.cat) for public use.

#### Build

	$ go build pastecat.go

#### Run

Default options:

	$ ./pastecat

Custom options:

	$ ./pastecat -u http://my.site -l hostname:80 -d /tmp/paste -t 1h -s 2M -i 6

It will stay in the foreground and log paste activity and errors.

#### Use

Set up an alias for easy usage:

	$ alias pcat='curl -F "paste=<-" http://paste.cat'

Upload a new paste via standard input:

	$ echo foo | pcat
	http://paste.cat/a63d03b9

Fetch it:

	$ curl ̣http://paste.cat/a63d03b9
	foo
