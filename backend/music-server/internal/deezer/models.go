package deezer

import "strings"

const (
	QualityFLAC    = "FLAC"
	QualityMP3_320 = "MP3_320"
	QualityMP3_256 = "MP3_256"
	QualityMP3_128 = "MP3_128"
)

type UserInfo struct {
	ID            uint64 `json:"id"`
	Name          string `json:"name"`
	Image         string `json:"image,omitempty"`
	IsFreeAccount bool   `json:"isFreeAccount"`
}

type Track struct {
	ID            int64   `json:"id"`
	Title         string  `json:"title"`
	Artist        string  `json:"artist"`
	ArtistID      int64   `json:"artistId,omitempty"`
	Album         string  `json:"album"`
	AlbumID       int64   `json:"albumId,omitempty"`
	Duration      float64 `json:"duration"`
	TrackPosition int     `json:"trackPosition,omitempty"`
	DiskNumber    int     `json:"diskNumber,omitempty"`
	ReleaseDate   string  `json:"releaseDate,omitempty"`
	Genre         string  `json:"genre,omitempty"`
	CoverSmall    string  `json:"coverSmall,omitempty"`
	CoverMedium   string  `json:"coverMedium,omitempty"`
	CoverBig      string  `json:"coverBig,omitempty"`
	CoverXL       string  `json:"coverXl,omitempty"`
	Link          string  `json:"link,omitempty"`
	Preview       string  `json:"preview,omitempty"`
}

func (t *Track) BestCoverURL() string {
	if t.CoverXL != "" {
		return t.CoverXL
	}
	if t.CoverBig != "" {
		return t.CoverBig
	}
	if t.CoverMedium != "" {
		return t.CoverMedium
	}
	return t.CoverSmall
}

type Album struct {
	ID          int64   `json:"id"`
	Title       string  `json:"title"`
	Artist      string  `json:"artist"`
	ArtistID    int64   `json:"artistId,omitempty"`
	CoverSmall  string  `json:"coverSmall,omitempty"`
	CoverMedium string  `json:"coverMedium,omitempty"`
	CoverBig    string  `json:"coverBig,omitempty"`
	CoverXL     string  `json:"coverXl,omitempty"`
	ReleaseDate string  `json:"releaseDate,omitempty"`
	Genre       string  `json:"genre,omitempty"`
	NbTracks    int     `json:"nbTracks,omitempty"`
	Tracks      []Track `json:"tracks,omitempty"`
}

func (a *Album) BestCoverURL() string {
	if a.CoverXL != "" {
		return a.CoverXL
	}
	if a.CoverBig != "" {
		return a.CoverBig
	}
	if a.CoverMedium != "" {
		return a.CoverMedium
	}
	return a.CoverSmall
}

type Playlist struct {
	ID          int64   `json:"id"`
	Title       string  `json:"title"`
	Creator     string  `json:"creator,omitempty"`
	CoverSmall  string  `json:"coverSmall,omitempty"`
	CoverMedium string  `json:"coverMedium,omitempty"`
	CoverBig    string  `json:"coverBig,omitempty"`
	CoverXL     string  `json:"coverXl,omitempty"`
	NbTracks    int     `json:"nbTracks,omitempty"`
	Tracks      []Track `json:"tracks,omitempty"`
}

func (p *Playlist) BestCoverURL() string {
	if p.CoverXL != "" {
		return p.CoverXL
	}
	if p.CoverBig != "" {
		return p.CoverBig
	}
	if p.CoverMedium != "" {
		return p.CoverMedium
	}
	return p.CoverSmall
}

type Artist struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	PictureSmall  string `json:"pictureSmall,omitempty"`
	PictureMedium string `json:"pictureMedium,omitempty"`
	PictureBig    string `json:"pictureBig,omitempty"`
	PictureXL     string `json:"pictureXl,omitempty"`
	NbAlbums      int    `json:"nbAlbums,omitempty"`
	NbFans        int    `json:"nbFans,omitempty"`
}

func IsKnownQuality(quality string) bool {
	switch strings.ToUpper(strings.TrimSpace(quality)) {
	case QualityFLAC, QualityMP3_320, QualityMP3_256, QualityMP3_128:
		return true
	}
	return false
}

func QualityCode(quality string) int {
	switch strings.ToUpper(quality) {
	case QualityFLAC:
		return 9
	case QualityMP3_320:
		return 3
	case QualityMP3_256:
		return 5
	case QualityMP3_128:
		return 1
	default:
		return 3
	}
}

func QualityExt(quality string) string {
	switch strings.ToUpper(quality) {
	case QualityFLAC:
		return ".flac"
	default:
		return ".mp3"
	}
}

func FallbackQualities(quality string) []string {
	switch strings.ToUpper(quality) {
	case QualityFLAC:
		return []string{QualityMP3_320, QualityMP3_128}
	case QualityMP3_320:
		return []string{QualityMP3_128}
	case QualityMP3_256:
		return []string{QualityMP3_128}
	default:
		return nil
	}
}
