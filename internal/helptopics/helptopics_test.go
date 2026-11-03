package helptopics

import (
	"regexp"
	"testing"
)

func TestTopicsAreValid(t *testing.T) {
	name := regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
	seen := map[string]bool{}
	for _, topic := range All() {
		if !name.MatchString(topic.Name) || seen[topic.Name] {
			t.Errorf("invalid or repeated topic name %q", topic.Name)
		}
		seen[topic.Name] = true
		if topic.Short == "" || topic.Text == "" {
			t.Errorf("topic %q needs a short help and a text", topic.Name)
		}
	}
}
