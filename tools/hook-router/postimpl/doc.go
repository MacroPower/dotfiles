// Package postimpl models the post-implementation skill catalog: the
// slash commands Claude is offered when a plan's Stop gate fires. The
// catalog renders the Stop block-message listing the options and
// validates AskUserQuestion option labels against them, keeping the
// two views of the same list in sync by construction.
package postimpl
