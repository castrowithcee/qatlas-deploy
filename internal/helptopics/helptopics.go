// Package helptopics holds the short guides that ship inside the binary. The command line prints them as help
// topics.
package helptopics

import "github.com/castrowithcee/qatlas-kit/helptopics"

// All returns the topics in the order a newcomer reads them. The text is plain ASCII with one line per
// paragraph, list item, or command; the kit fits it to the width where it is shown.
func All() []helptopics.Topic {
	return []helptopics.Topic{
		{Name: "qdeploy", Short: "What qdeploy is and what it is for", Text: overview},
	}
}

const overview = `qdeploy runs as a service on a Linux host and keeps the Docker stacks of that host rolled out and up to date from its stacks repository.

Commands:
- qdeploy run: run the service in the foreground until it receives SIGTERM or SIGINT
- qdeploy status: show whether the service is running, its version and start time (run with sudo)
- qdeploy version: print the version
- qdeploy help <topic>: show a help topic`
