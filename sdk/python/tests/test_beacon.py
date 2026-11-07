"""runyard beacon: a reading lands on the Machines endpoint; GPU processes are named without their arguments."""
import sys
from types import SimpleNamespace

from conftest import get
from runyard.beacon import Machine
from runyard.cli import main


def test_beacon_once(server, capsys):
    assert main(["beacon", "--name", "box-1", "--once"]) == 0
    assert main(["agent", "--name", "box-2", "--once"]) == 0  # the old name, which services set up before keep using
    assert "now `runyard beacon`" in capsys.readouterr().err
    hosts = get(server + "/api/v1/hosts")
    assert [h["name"] for h in hosts] == ["box-1", "box-2"]
    assert isinstance(hosts[0]["stats"].get("gpus", []), list)


def test_proc_name_keeps_secrets_out():
    m = Machine()
    argv = {1: ["/usr/bin/python3", "-u", "train.py", "--lr", "3e-4"], 2: ["curl", "-H", "Authorization: Bearer xyz"]}
    m.psutil = SimpleNamespace(Process=lambda pid: SimpleNamespace(cmdline=lambda: argv[pid], name=lambda: "?", username=lambda: "ege"))
    assert m._proc(SimpleNamespace(pid=1, usedGpuMemory=2e9)) == {"pid": 1, "name": "python3 train.py", "user": "ege", "mem_gb": 2.0}
    assert m._proc(SimpleNamespace(pid=2, usedGpuMemory=None))["name"] == "curl"


def test_gpu_without_nvml_is_reported(monkeypatch, capfd):
    from runyard import core
    from runyard.beacon import Machine

    monkeypatch.setitem(sys.modules, "pynvml", None)  # import pynvml raises ImportError
    monkeypatch.setattr(core, "_warned", set())
    monkeypatch.setattr(core.shutil, "which", lambda name: None)
    Machine()
    assert "nvidia-ml-py" not in capfd.readouterr().err  # no GPU here: nothing to say
    monkeypatch.setattr(core.shutil, "which", lambda name: "/usr/bin/nvidia-smi")
    Machine()
    core._System()
    assert capfd.readouterr().err.count("nvidia-ml-py") == 1  # once, whichever asks
