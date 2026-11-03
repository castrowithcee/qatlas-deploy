// Package helptopics holds the short guides that ship inside the binary. The command line prints them as help
// topics.
package helptopics

import "github.com/castrowithcee/qatlas-kit/helptopics"

// All returns the topics in the order a newcomer reads them. The text is plain ASCII with one line per
// paragraph, list item, or command; the kit fits it to the width where it is shown.
func All() []helptopics.Topic {
	return []helptopics.Topic{
		{Name: "qdeploy", Short: "What qdeploy is and what it is for", Text: overview},
		{Name: "credentials", Short: "The deploy key and stored credentials", Text: credentialsTopic},
	}
}

const overview = `qdeploy runs as a service on a Linux host and keeps the Docker stacks of that host rolled out and up to date from its stacks repository.

Commands:
- qdeploy run: run the service in the foreground until it receives SIGTERM or SIGINT
- qdeploy status: show whether the service is running, its version and start time (run with sudo)
- qdeploy key generate|show: create the SSH deploy key and show its public part (run with sudo)
- qdeploy credential set|list|delete: manage registry tokens and notification credentials (run with sudo)
- qdeploy version: print the version
- qdeploy help <topic>: show a help topic`

const credentialsTopic = `The service keeps its own secrets in a private store that only the service user can read. The commands need sudo because they talk to the service over its root-only channel.

Deploy key:
- qdeploy key generate: create an Ed25519 key in the service and print its public part and SHA256 fingerprint
- qdeploy key generate --replace: replace an existing key; without --replace an existing key is kept and the command refuses
- qdeploy key show: print the public part again
- Enter the public part as a deploy key in the git platform. The private key never leaves the service.

Credentials:
- qdeploy credential set <name>: store a registry token or notification credential; the value is read without echo at a terminal, or from standard input when piped, and never from an argument
- qdeploy credential list: show names and modification times, never values
- qdeploy credential delete <name>: remove a credential
- A name has 1 to 64 characters: lowercase letters, digits, '.', '_' or '-', starting with a letter.
- A stored value is not shown again and is removed from the service's log output.`
