package presets

// ClipPart is one piece of a cut video, in source seconds.
type ClipPart struct {
	Start float64 `json:"start"`
	End   float64 `json:"end"`
	// Blank is a black gap of the part's length instead of source footage.
	Blank  bool    `json:"blank,omitempty"`
	Volume float64 `json:"volume"`
	Muted  bool    `json:"muted,omitempty"`
	Speed  float64 `json:"speed"`
}

const (
	MinClipSpeed  = 0.25
	MaxClipSpeed  = 4
	MaxClipVolume = 2
	MaxClipFade   = 10
)

// ClipSpeeds are the speeds the clip editor offers.
var ClipSpeeds = []float64{0.25, 0.5, 0.75, 1, 1.25, 1.5, 2, 3, 4}

func (p *ClipPart) PlaySpeed() float64 {
	if !finite(p.Speed) || p.Speed <= 0 {
		return 1
	}
	return clamp(p.Speed, MinClipSpeed, MaxClipSpeed)
}

func (p *ClipPart) PlayVolume() float64 {
	switch {
	case p.Muted:
		return 0
	case finite(p.Volume) && p.Volume >= 0:
		return min(p.Volume, MaxClipVolume)
	}
	return 1
}

// Length is the part's length in the output.
func (p *ClipPart) Length() float64 { return max(0, p.End-p.Start) / p.PlaySpeed() }

func (p *ClipPart) IsPlain() bool {
	return !p.Blank && !p.Muted && p.PlayVolume() == 1 && p.PlaySpeed() == 1
}

// ClipMusic is a song laid under the cut.
type ClipMusic struct {
	Path string `json:"path"`
	// Offset is where in the clip the music starts, From where in the song.
	Offset float64 `json:"offset,omitempty"`
	From   float64 `json:"from,omitempty"`
	Length float64 `json:"length"`
	Volume float64 `json:"volume"`
	Fade   bool    `json:"fade"`
}

const (
	DefaultMusicVolume = 0.6
	MusicFadeSeconds   = 1
)

func (m *ClipMusic) PlayVolume() float64 {
	if finite(m.Volume) && m.Volume >= 0 {
		return min(m.Volume, 2)
	}
	return DefaultMusicVolume
}

func (m *ClipMusic) PlayLength(clipLength float64) float64 {
	return max(0, min(m.Length-m.From, clipLength-m.Offset))
}

// ClipEdit is the clip editor's result: parts, fades and music.
type ClipEdit struct {
	Parts   []ClipPart `json:"parts"`
	FadeIn  float64    `json:"fadeIn,omitempty"`
	FadeOut float64    `json:"fadeOut,omitempty"`
	Music   *ClipMusic `json:"music,omitempty"`
}

// ClipOf returns a plain cut from start to end.
func ClipOf(start, end float64) *ClipEdit {
	return &ClipEdit{Parts: []ClipPart{{Start: start, End: end, Volume: 1, Speed: 1}}}
}

func (c *ClipEdit) Clone() *ClipEdit {
	if c == nil {
		return nil
	}
	d := *c
	d.Parts = append([]ClipPart(nil), c.Parts...)
	if c.Music != nil {
		m := *c.Music
		d.Music = &m
	}
	return &d
}

// Length is the output length in seconds.
func (c *ClipEdit) Length() float64 {
	var l float64
	for i := range c.Parts {
		l += c.Parts[i].Length()
	}
	return l
}

// Start is the first source second used.
func (c *ClipEdit) Start() float64 {
	for _, p := range c.Parts {
		if !p.Blank {
			return p.Start
		}
	}
	return 0
}

// End is the last source second used.
func (c *ClipEdit) End() float64 {
	for i := len(c.Parts) - 1; i >= 0; i-- {
		if !c.Parts[i].Blank {
			return c.Parts[i].End
		}
	}
	return 0
}

// ClipFade clamps a fade length.
func ClipFade(seconds float64) float64 {
	if !finite(seconds) {
		return 0
	}
	return clamp(seconds, 0, MaxClipFade)
}

// IsPlainCut: one plain part, no fades, no music (a stream copy can do it).
func (c *ClipEdit) IsPlainCut() bool {
	return len(c.Parts) == 1 && c.Parts[0].IsPlain() && ClipFade(c.FadeIn) == 0 && ClipFade(c.FadeOut) == 0 && c.Music == nil
}
