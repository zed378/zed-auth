package mail

import (
	"fmt"
	"strings"
)

// The two messages Phase 1 sends.
//
// Both are plain text and both put the URL on its own line, in full. That is
// not a stylistic preference: a recipient judging whether a message is genuine
// has almost nothing to go on except the address it points at, and a link
// hidden behind "click here" removes the one signal they have. Phishing
// imitates the styled version; it cannot imitate a URL.

// Invitation is the message that carries an invite link.
//
// It names who invited them and which organization, because "you have been
// invited" from an unfamiliar domain is indistinguishable from an attack.
func Invitation(to, organization, inviter, link string, validFor string) Message {
	var b strings.Builder
	fmt.Fprintf(&b, "You have been invited to join %s.\n\n", organization)
	if inviter != "" {
		fmt.Fprintf(&b, "The invitation was sent by %s.\n\n", inviter)
	}
	b.WriteString("To set your password and sign in, open this link:\n\n")
	b.WriteString(link + "\n\n")
	fmt.Fprintf(&b, "The link can be used once and expires in %s.\n\n", validFor)
	b.WriteString("If you were not expecting this, you can ignore this message — " +
		"the invitation cannot be used without opening the link.\n")

	return Message{
		To:      to,
		Subject: "You have been invited to " + organization,
		Body:    b.String(),
	}
}

// PasswordReset is the message that carries a reset link.
//
// It says explicitly that nothing has changed yet. A reset email arriving
// unrequested is the first sign of an account under attack, and the recipient's
// immediate question is whether their password already changed.
func PasswordReset(to, organization, link string, validFor string) Message {
	var b strings.Builder
	fmt.Fprintf(&b, "Somebody asked to reset the password for this address at %s.\n\n", organization)
	b.WriteString("To choose a new password, open this link:\n\n")
	b.WriteString(link + "\n\n")
	fmt.Fprintf(&b, "The link can be used once and expires in %s.\n\n", validFor)
	b.WriteString("If this was not you, nothing has happened yet — your password has not " +
		"changed and this link will expire on its own. If you receive these repeatedly, " +
		"tell your administrator.\n")

	return Message{
		To:      to,
		Subject: "Reset your password at " + organization,
		Body:    b.String(),
	}
}

// LoginAnomaly tells a user their account was signed in to in an unfamiliar way
// (P3-08).
//
// It says WHAT was unusual, in words a person recognises, and it gives exactly
// one action. Two things it deliberately does not do:
//
//   - It does not say "your account may be compromised". Most of these are a new
//     laptop or a trip, and a notice that cries wolf in its first sentence is one
//     people learn to delete unread — the outcome the card's step 5 names as
//     worse than no notice at all.
//   - It does not carry the full IP address. The location is coarse on purpose,
//     and a precise address in an email is a detail that outlives its usefulness
//     in somebody's inbox.
func LoginAnomaly(to, organization string, reasons []string, when, where, reportLink, validFor string) Message {
	var b strings.Builder
	fmt.Fprintf(&b, "Your %s account was just signed in to", organization)
	if where != "" {
		fmt.Fprintf(&b, " from %s", where)
	}
	fmt.Fprintf(&b, " at %s.\n\n", when)

	b.WriteString("We are telling you because this sign-in looked different from your usual ones:\n\n")
	for _, r := range reasons {
		b.WriteString("  - " + r + "\n")
	}

	b.WriteString("\nIf this was you — a new device, or you are travelling — you do not need to do anything.\n\n")
	b.WriteString("If it was not you, open this link. It will sign your account out everywhere " +
		"and send you a link to choose a new password:\n\n")
	b.WriteString(reportLink + "\n\n")
	fmt.Fprintf(&b, "The link expires in %s.\n", validFor)

	return Message{
		To:      to,
		Subject: "New sign-in to your " + organization + " account",
		Body:    b.String(),
	}
}
