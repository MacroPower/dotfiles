// Package promptguard finds the tool calls whose prompt no one answers.
// A hook whose rule forces a prompt denies such a call instead.
//
// Claude Code shows the permission prompt of an agent's tool call in
// the session that started the agent. The call waits until a person
// answers. The agents of the Workflow tool run for hours with no
// person at that session. One prompt then holds its agent, and the
// Workflow with it, until someone stops the run. [Unattended] reports
// whether a call comes from such an agent, and [Reason] gives the
// reason of the deny that takes the place of the prompt.
//
// # Workflow Agents
//
// [Unattended] reads two marks of a Workflow agent from the hook
// payload of a call. Either mark is enough.
//
//   - The agent type is [WorkflowAgentType]. Claude Code gives that
//     type to each agent that a Workflow script starts without an
//     agent type of its own.
//   - The transcript of the agent sits in the workflows directory
//     under the subagents directory of the session. Claude Code writes
//     the transcript of every Workflow agent there, whatever the type
//     of the agent.
//
// Claude Code documents neither mark. This package follows Claude Code
// 2.1.319. A call that carries neither mark keeps its prompt.
package promptguard
