package gemara

import "sync"

// SugaredControl wraps the generated Control with cached
// cross-reference lookups.
type SugaredControl struct {
	Control

	referencesOnce  sync.Once
	referencesCache []string
}

// Sugar wraps this Control in a SugaredControl for convenient
// cached helper access.
func (c Control) Sugar() *SugaredControl {
	return &SugaredControl{Control: c}
}

func (c *SugaredControl) GetMappingReferences() []string {
	c.referencesOnce.Do(func() {
		for _, ref := range c.Guidelines {
			c.referencesCache = append(c.referencesCache, ref.ReferenceId)
		}
		for _, ref := range c.Threats {
			c.referencesCache = append(c.referencesCache, ref.ReferenceId)
		}
	})
	return c.referencesCache
}
