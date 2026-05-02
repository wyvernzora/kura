package trash

import (
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/wyvernzora/kura/internal/domain/refs"
)

type Meta struct {
	ID        ulid.ULID
	Episode   refs.Episode
	TrashedAt time.Time
	Record    Record
}

type Record struct {
	Path       string
	Source     string
	Resolution string
	Codec      string
	Size       int64
	MTime      time.Time
	Companions []Companion
}

type Companion struct {
	Path     string
	Role     string
	Language string
	Label    string
	Size     int64
	MTime    time.Time
}
