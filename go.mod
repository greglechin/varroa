module gitlab.com/passelecasque/varroa

require (
	bazil.org/fuse v0.0.0-20180421153158-65cc252bf669
	github.com/DataDog/zstd v1.3.5 // indirect
	github.com/Sereal/Sereal v0.0.0-20181211220259-509a78ddbda3 // indirect
	github.com/asdine/storm v2.1.2+incompatible
	github.com/blend/go-sdk v1.20220411.3 // indirect
	github.com/briandowns/spinner v0.0.0-20181029155426-195c31b675a7
	github.com/djherbis/times v1.1.0
	github.com/docopt/docopt-go v0.0.0-20180111231733-ee0de3bc6815
	github.com/dsnet/compress v0.0.0-20171208185109-cc9eb1d7ad76 // indirect
	github.com/dustin/go-humanize v1.0.0
	github.com/fhs/gompd v2.0.0+incompatible
	github.com/frankban/quicktest v1.9.0 // indirect
	github.com/goji/httpauth v0.0.0-20160601135302-2da839ab0f4d
	github.com/golang/freetype v0.0.0-20170609003504-e2365dfdc4a0 // indirect
	github.com/gorilla/mux v1.6.2
	github.com/gorilla/websocket v1.4.0
	github.com/gregdel/pushover v0.0.0-20180208231006-1e03358b8e7e
	github.com/howeyc/gopass v0.0.0-20170109162249-bf9dde6d0d2c
	github.com/jasonlvhit/gocron v0.0.0-20180312192515-54194c9749d4
	github.com/jinzhu/now v0.0.0-20181116074157-8ec929ed50c3
	github.com/mewkiz/flac v1.0.5
	github.com/mgutz/ansi v0.0.0-20170206155736-9520e82c474b
	github.com/mholt/archiver v3.1.1+incompatible
	github.com/nwaples/rardecode v1.0.0 // indirect
	github.com/pierrec/lz4 v2.5.0+incompatible // indirect
	github.com/pkg/errors v0.9.1
	github.com/russross/blackfriday v2.0.0+incompatible
	github.com/sevlyar/go-daemon v0.1.5
	github.com/stretchr/testify v1.7.0
	github.com/tdewolff/minify v2.3.6+incompatible
	github.com/tdewolff/parse v2.3.4+incompatible // indirect
	github.com/tdewolff/test v1.0.0 // indirect
	github.com/ulikunitz/xz v0.5.5 // indirect
	github.com/vmihailenco/msgpack v4.0.1+incompatible // indirect
	github.com/wcharczuk/go-chart v2.0.1+incompatible
	github.com/xi2/xz v0.0.0-20171230120015-48954b6210f8 // indirect
	gitlab.com/catastrophic/assistance v0.32.1
	gitlab.com/catastrophic/go-ircevent v0.1.0
	gitlab.com/passelecasque/obstruction v0.15.10
	go.etcd.io/bbolt v1.3.4 // indirect
	golang.org/x/net v0.0.0-20211216030914-fe4d6282115f
	gopkg.in/yaml.v2 v2.4.0
)

go 1.13

//replace gitlab.com/catastrophic/assistance => ../../catastrophic/assistance
//replace gitlab.com/passelecasque/obstruction => ../../passelecasque/obstruction
