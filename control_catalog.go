package gemara

import "sync"

// SugaredControlCatalog wraps the generated ControlCatalog with
// pre-built indexes for efficient group, control, and requirement lookups.
type SugaredControlCatalog struct {
	ControlCatalog

	groupsOnce  sync.Once
	groupsCache []string

	sugarControlsOnce  sync.Once
	sugarControlsCache []*SugaredControl

	controlsByGroupOnce  sync.Once
	controlsByGroupCache map[string][]*SugaredControl

	requirementsOnce  sync.Once
	requirementsCache map[string][]AssessmentRequirement
}

// Sugar wraps this ControlCatalog in a SugaredControlCatalog for
// convenient cached helper access.
func (c ControlCatalog) Sugar() *SugaredControlCatalog {
	return &SugaredControlCatalog{ControlCatalog: c}
}

// SugaredControls returns all controls as cached SugaredControl instances.
func (c *SugaredControlCatalog) SugaredControls() []*SugaredControl {
	c.sugarControlsOnce.Do(func() {
		c.sugarControlsCache = make([]*SugaredControl, len(c.Controls))
		for i := range c.Controls {
			c.sugarControlsCache[i] = &SugaredControl{Control: c.Controls[i]}
		}
	})
	return c.sugarControlsCache
}

func (c *SugaredControlCatalog) GetGroupNames() []string {
	c.groupsOnce.Do(func() {
		for _, group := range c.Groups {
			c.groupsCache = append(c.groupsCache, group.Title)
		}
	})
	return c.groupsCache
}

func (c *SugaredControlCatalog) GetControlsForGroup(group string) []*SugaredControl {
	c.controlsByGroupOnce.Do(func() {
		c.controlsByGroupCache = make(map[string][]*SugaredControl)
		for _, sc := range c.SugaredControls() {
			c.controlsByGroupCache[sc.Group] = append(
				c.controlsByGroupCache[sc.Group], sc,
			)
		}
	})
	return c.controlsByGroupCache[group]
}

func (c *SugaredControlCatalog) GetRequirementForApplicability(applicability string) []AssessmentRequirement {
	c.requirementsOnce.Do(func() {
		c.requirementsCache = make(map[string][]AssessmentRequirement)
		for _, control := range c.Controls {
			for _, req := range control.AssessmentRequirements {
				for _, app := range req.Applicability {
					c.requirementsCache[app] = append(
						c.requirementsCache[app], req,
					)
				}
			}
		}
	})
	return c.requirementsCache[applicability]
}
