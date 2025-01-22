package config

// Opts for individual xDS implementations that can be
// utilized through the functional opts pattern.
type Opts struct {
	// If true respond to ADS requests with a guaranteed resource ordering
	Ordered bool
}

func NewOpts() Opts {
	return Opts{
		Ordered: false,
	}
}

// Each xDS implementation should implement their own functional opts.
// It is recommended that config values be added in this package specifically,
// but the individual opts functions should be in their respective
// implementation package so the import looks like the following:
//
// `sotw.WithOrderedADS()`
// `delta.WithOrderedADS()`
//
// this allows for easy inference as to which opt applies to what implementation.
type XDSOption func(*Opts)
