// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package git

import "context"

// Network states whether an invocation may contact remote repositories.
type Network bool

const (
	// Offline allows no transport: a missing repository is not cloned, and
	// objects missing from a partial clone are not fetched on demand.
	Offline Network = false
	// Online allows the transports that the configuration and the
	// environment permit, including fetching objects missing from a partial
	// clone on demand.
	Online Network = true
)

// Environment variables that control network access.
const (
	// noLazyFetch stops git from fetching objects missing from a partial
	// clone on demand. Git has supported it since the security releases of
	// May 2024, such as 2.39.4; older releases ignore it.
	noLazyFetch = "GIT_NO_LAZY_FETCH=1"
	// lazyFetch overrides noLazyFetch.
	lazyFetch = "GIT_NO_LAZY_FETCH=0"
	// noTransport allows no transport at all, whatever protocol.<name>.allow
	// says. It also stops lazy fetches in releases that ignore noLazyFetch.
	noTransport = "GIT_ALLOW_PROTOCOL="
)

// env returns the variables that put an invocation in mode n. They are
// passed as Cmd.Env, after the environment of the Runner, so an Offline
// invocation stays offline whatever WithEnv added.
func (n Network) env() []string {
	if n == Online {
		return []string{lazyFetch}
	}
	return []string{noLazyFetch, noTransport}
}

// runOffline runs git with args in dir, like Run, but allows no transport.
// Every helper uses it, except the ones documented to use the network and
// Commit, whose hooks may use the network as configured.
func (r *Runner) runOffline(ctx context.Context, dir string, args ...string) (string, error) {
	return r.Exec(ctx, Cmd{Dir: dir, Args: args, Env: Offline.env()})
}
