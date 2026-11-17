// Package remote will own injecting and supervising the in-session
// worker (ad-remote.exe): CreateRemoteThread launch, the
// remote_process_launcher equivalent for starting apps inside the
// shadow session, and the dialog_auto_closer.
//
// The worker itself is real code under cmd/ad-remote; what is not
// bound yet is cross-session injection, which needs a live shadow
// session to exist first.
package remote
