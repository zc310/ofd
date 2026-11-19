"""验证容器启动参数，不依赖 Docker 或已发布的镜像。"""

import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest


SCRIPT = Path(__file__).with_name("run.sh")


class ContainerRunnerTest(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="ofd-ci-test-")
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.home = self.root / "home with spaces"
        self.workspace = self.root / "workspace with spaces"
        self.workspace.mkdir()
        self.arguments = self.root / "docker-arguments.json"
        docker = self.root / "docker"
        docker.write_text(
            "#!/usr/bin/env python3\n"
            "import json, os, sys\n"
            "with open(os.environ['DOCKER_ARGUMENTS'], 'w') as output:\n"
            "    json.dump(sys.argv[1:], output)\n"
        )
        docker.chmod(0o755)
        self.env = {
            **os.environ,
            "HOME": str(self.home),
            "GITHUB_WORKSPACE": str(self.workspace),
            "OFD_CI_OUTPUT_DIR": str(self.root / "output"),
            "OFD_CI_IMAGE": "ofd-ci:local",
            "DOCKER_ARGUMENTS": str(self.arguments),
            "PATH": str(self.root) + os.pathsep + os.environ["PATH"],
        }

    def invoke(self, *arguments):
        subprocess.run(["bash", str(SCRIPT), *arguments], env=self.env, check=True)
        return json.loads(self.arguments.read_text())

    def test_preserves_arguments_and_uses_image_toolchain(self):
        """含空格与分号的命令参数应原样传入，不挂载或替换宿主机 GOROOT。"""
        command = ["printf", "%s", "argument with spaces", "literal;semicolon"]
        arguments = self.invoke(*command)
        self.assertEqual(arguments[-len(command):], command)
        self.assertIn(f"{self.home}:{self.home}", arguments)
        self.assertIn(f"{self.workspace}:{self.workspace}", arguments)
        self.assertIn(f"{self.root / 'output'}:/tmp/ofd", arguments)
        self.assertIn(f"{os.getuid()}:{os.getgid()}", arguments)
        self.assertIn("GOTOOLCHAIN=local", arguments)
        self.assertFalse(any(value.startswith("CGO_ENABLED=") for value in arguments))
        self.assertIn(f"GOPATH={self.home}/go", arguments)
        self.assertIn(f"GOCACHE={self.home}/.cache/go-build", arguments)
        self.assertFalse(any(value.startswith("GOROOT=") for value in arguments))
        self.assertFalse(any(value.startswith("PATH=") for value in arguments))
        self.assertIn('export PATH="$GOROOT/bin:$GOPATH/bin:$PATH"', arguments[-len(command)-2])
        self.assertTrue((self.home / "go").is_dir())
        self.assertTrue((self.home / ".cache/go-build").is_dir())

    def test_supports_image_override_and_default_shell(self):
        """本地调试可覆盖镜像，未提供命令时启动 Bash。"""
        self.env["OFD_CI_IMAGE"] = "builder:test"
        arguments = self.invoke()
        self.assertIn("builder:test", arguments)
        self.assertEqual(arguments[-1], "bash")

    def test_supports_command_scoped_cgo(self):
        """CGO 开关只应用到指定命令，不成为容器的全局默认值。"""
        for value in ("0", "1"):
            with self.subTest(cgo=value):
                command = ["env", f"CGO_ENABLED={value}", "go", "build"]
                arguments = self.invoke(*command)
                self.assertEqual(arguments[-len(command):], command)
                image_index = arguments.index("ofd-ci:local")
                self.assertNotIn(f"CGO_ENABLED={value}", arguments[:image_index])

    def test_requires_explicit_workspace(self):
        """缺少工作区时应在运行 Docker 前失败。"""
        self.env.pop("GITHUB_WORKSPACE")
        result = subprocess.run(
            ["bash", str(SCRIPT), "go", "version"],
            env=self.env, capture_output=True, text=True,
        )
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("GITHUB_WORKSPACE", result.stderr)
        self.assertFalse(self.arguments.exists())


if __name__ == "__main__":
    unittest.main()
