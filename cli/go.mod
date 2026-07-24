module github.com/wyvernzora/kura/cli

go 1.26.3

require (
	github.com/AlecAivazis/survey/v2 v2.3.7
	github.com/alecthomas/kong v1.15.0
	github.com/jedib0t/go-pretty/v6 v6.8.2
	github.com/oklog/ulid/v2 v2.1.1
	github.com/pelletier/go-toml/v2 v2.3.1
	github.com/wyvernzora/kura/services/library-manager v0.0.0-00010101000000-000000000000
	golang.org/x/term v0.44.0
	golang.org/x/tools v0.47.0
	rsc.io/script v0.0.2
)

require (
	cloud.google.com/go v0.123.0 // indirect
	github.com/creack/pty v1.1.24 // indirect
	github.com/google/renameio/v2 v2.0.2 // indirect
	github.com/hashicorp/golang-lru/v2 v2.0.7 // indirect
	github.com/kballard/go-shellquote v0.0.0-20180428030007-95032a82bc51 // indirect
	github.com/mattn/go-colorable v0.1.12 // indirect
	github.com/mattn/go-isatty v0.0.22 // indirect
	github.com/mattn/go-runewidth v0.0.16 // indirect
	github.com/mgutz/ansi v0.0.0-20170206155736-9520e82c474b // indirect
	github.com/rivo/uniseg v0.4.7 // indirect
	github.com/santhosh-tekuri/jsonschema/v6 v6.0.2 // indirect
	golang.org/x/sys v0.46.0 // indirect
	golang.org/x/text v0.38.0 // indirect
)

replace github.com/wyvernzora/kura/services/library-manager => ../services/library-manager
