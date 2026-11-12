# Product

<!-- impeccable:product-schema 1 -->

## Platform

ios

Native SwiftUI app for iPhone (iOS 17+) with a companion Apple Watch app (watchOS). The watch is the preferred surface when worn; the iPhone must work as a complete deck on its own.

## Users

One developer who runs several AI coding agents at once (90% Claude Code, the rest Codex, Antigravity CLI `agy` and the Antigravity IDE agent), started by hand in terminals inside the Antigravity IDE, across two computers: a Mac and a Linux machine. Sessions run for hours or days in several repos and worktrees.

Their job on this app: know which agent needs them, approve or deny a permission request, or pick the answer to an agent's question, without walking back to the computer, and without carrying an extra gadget.

The iPhone is used in two situations, equally often:

- away from the desk (another room, outside): a quick glance, approve, put the phone away;
- standing next to the keyboard as a second screen, open all the time.

## Product Purpose

The mobile and wrist client of Towerman. It shows every agent session from every computer in one place, raises the ones that are blocked on the user, and lets the user answer them from the phone or the watch. Success: no agent sits blocked unnoticed, and answering one takes seconds.

## Positioning

The answer travels through the agent's own hook (Claude Code `PermissionRequest`), not through simulated keystrokes, so it works for any session in any terminal or IDE. The terminal shows the same request at the same time; whoever answers first wins, and the other side clears. Answers to multiple-choice questions are delivered as the chosen option, not typed.

## Operating Context

- Server side: Towerman (Go, user's own project) runs on each computer, records sessions through the hooks of Claude Code, Codex and Antigravity, and holds permission requests while waiting for a decision (up to 10 minutes). Its network port (7352) speaks the Agents Deck `/v1` protocol (state, server-sent events, answer, focus, input) to paired devices; on the Mac the server ships inside the menu bar app.
- Network: same Wi-Fi at home; Tailscale when away. One app connects to several computers at once (planned: Mac + Linux merged in one list).
- Free Apple developer account today: local notifications only, app reinstalled every 7 days. Push (APNs) needs the paid account and comes later.
- Inspiration the user chose: the agents-deck project (tiles per agent, orange = needs you, activity states, "hungry" when a finished turn is unseen).

## Capabilities and Constraints

- Per agent: title, AI session title or last prompt, status (`starting`, `running`, `waiting`, `idle`, `error`, `exited`), host, provider, branch, folder, activity, context use, cost, unseen flag, capabilities.
- Pending request: kind (`permission`, `question`, `input`), title, detail (a command, a file, or the question text), options with one primary.
- Actions: answer (when the agent's capabilities include it), focus the terminal on the computer, interrupt (not yet supported by Towerman).
- Some providers are read-only (Antigravity IDE agent, Codex app): the UI must show them without answer buttons.
- Pairing is by QR code (one-time code, per-device token, Bonjour to find a computer whose IP changed); TLS is planned.
- Undecided: multi-server list UI, Live Activity, widgets and complications (all planned, not built).

## Brand Commitments

- Name: **Towerman** (the app is renamed from "Agent Deck" to match the server).
- UI language: Brazilian Portuguese.

## Evidence on Hand

- Working end-to-end flow verified on the user's iPhone 14 Pro Max: real Claude Code request shown and approved from the phone.
- No icons, logo, screenshots or marketing assets exist yet. Do not invent usage numbers or testimonials.

## Product Principles

1. The agent that needs you is the first thing you see; everything else waits.
2. Answering is one tap, and a stale or already-answered request can never be approved by mistake.
3. Show what the command is before asking to allow it; never hide the risk behind a summary.
4. Setup appears only when something is wrong; a connected app is a deck, not a settings page.
5. Every computer and every provider shows up the same way; read-only agents are visible, not hidden.

## Accessibility & Inclusion

Glanceable at arm's length and on a 41–46 mm watch: Dynamic Type, VoiceOver labels on every action, and status never carried by color alone.
