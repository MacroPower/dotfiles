// Package gittest keeps the repository of the developer out of the
// tests that run git.
//
// Variables of the environment point git at a repository. With GIT_DIR
// set, a git command acts on the repository that GIT_DIR names, not on
// the repository of its working directory. The setup of a test then
// writes the config of that repository and commits to its current
// branch. git sets GIT_DIR and GIT_INDEX_FILE for the hooks of a commit
// in a linked worktree. A commit hook that runs the tests passes both
// variables on.
//
// A test binary covers one package, so each test package that runs git
// calls [UnsetRepositoryEnv] in a TestMain of its own:
//
//	func TestMain(m *testing.M) {
//		err := gittest.UnsetRepositoryEnv(context.Background())
//		if err != nil {
//			panic(err)
//		}
//
//		os.Exit(m.Run())
//	}
//
// Each git command of the tests and of the code under test then acts
// on the repository that git finds from the directory of the command.
// One test of the package calls [CheckRepositoryEnv], which fails when
// TestMain leaves one of the variables to the environment.
package gittest
