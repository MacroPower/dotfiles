/*
 * A ` mark in a block comment.
 */
// A span wraps over `it may
// stop` here, and then it may stop.
const tick = /`/;
// A span wraps over `it may
// stop` here, and then it may stop.
const text = "`";
// A span wraps over `it may
// stop` here, and then it may stop.
const trailing = 1; // A ` mark in a trailing comment.
// A span wraps over `it may
// stop` here, and then it may stop.
const quoted = `
// Quote with \` marks
`;
// It may help after the quote.
const fence = `
// ~~~
// end`;
// It may help after the fence.
const nested = `a ${"`"}
// ~~~
// end`;
// It may help after the nested template.
const deep = `a ${{ k: 1 }.k + "`"}
// ~~~
// end`;
// It may help after the braces.
const inner = `a ${
  // A span wraps over `it may
  // stop` here, and then it may stop.
  1
} b`;
const kind = typeof /`/;
// A span wraps over `it may
// stop` here, and then it may stop.
const single = '`"';
// A span wraps over `it may
// stop` here, and then it may stop.
const escaped = "'\"`";
// A span wraps over `it may
// stop` here, and then it may stop.
const klass = /[/`]/;
// A span wraps over `it may
// stop` here, and then it may stop.
const closed = /[a]`/;
// A span wraps over `it may
// stop` here, and then it may stop.
const slash = /\/`/;
// A span wraps over `it may
// stop` here, and then it may stop.
/`/.test("x");
// A span wraps over `it may
// stop` here, and then it may stop.
/*
// The block ends here. */
/`/.test("x");
// A span wraps over `it may
// stop` here, and then it may stop.
/*
// The block ends here. */
const blk = `
// ~~~
// end`;
// It may help after the block.
/*
// The block ends here. */
// The comment goes on.
/`/.test("x");
// A span wraps over `it may
// stop` here, and then it may stop.
const after = 1; /*
// The block ends here. */
/`/.test("x");
// A span wraps over `it may
// stop` here, and then it may stop.
