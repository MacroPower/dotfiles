// Package gittest keeps the git config and the repository of the
// developer out of the tests that run git.
//
// Keys of the git config change what git does. With core.hooksPath set
// in the global config, each git commit of a test runs the hooks of
// the developer in the repository of the test. With commit.gpgSign
// set, a git commit fails when the gpg program cannot sign. git reads
// the system config and the global config for every repository, so
// the tests read them as well. git init also copies the hooks of the
// template directory that GIT_TEMPLATE_DIR names into each repository
// that it creates.
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
// calls [IsolateEnv] in a TestMain of its own:
//
//	func TestMain(m *testing.M) {
//		err := gittest.IsolateEnv(context.Background())
//		if err != nil {
//			panic(err)
//		}
//
//		os.Exit(m.Run())
//	}
//
// Each git command of the tests and of the code under test then skips
// the system config and the global config. It acts on the repository
// that git finds from the directory of the command. git init and git
// clone copy no template into a new repository.
//
// Tests of the package check its TestMain. One test calls
// [CheckConfigEnv], which fails when git still reads the system
// config, the global config, or the template directory. Another test
// calls [CheckRepositoryEnv], which fails when TestMain leaves a
// variable to the environment that points git at a repository.
package gittest
