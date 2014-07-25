## Paste.cat

A very simple and self-hosted pastebin service written in Go. Stores
zlib-compressed pastes in a filesystem hierarchy.

Designed to remove pastes after a certain period of time. Upon restart, file
modify times will be used to try to recover the removal time of each paste.

#### Build

	go build pastecat.go

#### Run

Default options:

	./pastecat

Custom options:

	./pastecat -u http://my.site -l hostname:80 -d /tmp/paste -t 1h -s 2M

It will stay in the foreground and log paste activity and errors.
