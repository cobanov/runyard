"""runyard agent: posts this machine's GPUs, CPU, memory and disk to the server every few seconds, training or not,
so the Machines page shows who is using a GPU (any process, not only runyard runs) and the server can tell you when
the machine goes silent. GPUs need nvidia-ml-py, CPU/RAM/disk and process names need psutil: pip install 'runyard[system]'."""
import os
import re
import sys
import time
import urllib.parse

from .client import request

_ARG = re.compile(r"^[\w.+/-]{1,80}$")  # a script or module name; anything else may be a secret passed as an argument


class Machine:
    def __init__(self, disk="/"):
        self.disk = disk
        self.nvml = self.psutil = None
        self.gpus = []
        try:
            import pynvml

            pynvml.nvmlInit()
            self.gpus = [pynvml.nvmlDeviceGetHandleByIndex(i) for i in range(pynvml.nvmlDeviceGetCount())]
            self.nvml = pynvml
        except Exception:
            pass
        try:
            import psutil

            psutil.cpu_percent()  # the first call only starts the measurement
            self.psutil = psutil
        except Exception:
            pass

    def read(self):
        """One reading in the shape POST /api/v1/hosts/{name} takes; what this machine cannot measure is left out."""
        v = {}
        ps = self.psutil
        if ps:
            v["cpu"] = ps.cpu_percent()
            v["cores"] = ps.cpu_count()
            vm = ps.virtual_memory()
            v["ram_gb"], v["ram_total_gb"] = (vm.total - vm.available) / 1e9, vm.total / 1e9
            du = ps.disk_usage(self.disk)
            v["disk_gb"], v["disk_total_gb"] = du.used / 1e9, du.total / 1e9
        try:
            v["load"] = os.getloadavg()[0]
        except (AttributeError, OSError):  # Windows
            pass
        v["gpus"] = [self._gpu(i, h) for i, h in enumerate(self.gpus)]
        return v

    def _gpu(self, i, h):
        n = self.nvml
        g = {"i": i}
        reads = {  # each on its own: consumer cards refuse some (power limit, processes)
            "name": lambda: _text(n.nvmlDeviceGetName(h)),
            "util": lambda: n.nvmlDeviceGetUtilizationRates(h).gpu,
            "mem_gb": lambda: n.nvmlDeviceGetMemoryInfo(h).used / 1e9,
            "mem_total_gb": lambda: n.nvmlDeviceGetMemoryInfo(h).total / 1e9,
            "temp_c": lambda: n.nvmlDeviceGetTemperature(h, 0),
            "power_w": lambda: n.nvmlDeviceGetPowerUsage(h) / 1000,
            "power_max_w": lambda: n.nvmlDeviceGetEnforcedPowerLimit(h) / 1000,
            "procs": lambda: [self._proc(p) for p in n.nvmlDeviceGetComputeRunningProcesses(h)],
        }
        for k, f in reads.items():
            try:
                g[k] = f()
            except Exception:
                pass
        return g

    def _proc(self, p):
        out = {"pid": p.pid, "name": "", "mem_gb": (p.usedGpuMemory or 0) / 1e9}
        try:
            proc = self.psutil.Process(p.pid)
            argv = proc.cmdline()
            prog = os.path.basename(argv[0]) if argv else proc.name()
            script = next((a for a in argv[1:] if not a.startswith("-")), "")
            out["name"] = f"{prog} {os.path.basename(script)}" if _ARG.match(script) else prog
            out["user"] = proc.username()
        except Exception:  # no psutil, or the process just ended
            pass
        return out


def _text(s):
    return s.decode() if isinstance(s, bytes) else s


def agent(server, key, name, interval=10, once=False, disk="/"):
    """Posts a reading every interval seconds until stopped; a failure is reported once and retried."""
    m = Machine(disk)
    url = f"{server}/api/v1/hosts/{urllib.parse.quote(name)}"
    print(f"runyard agent: {name} -> {server}, every {interval:g}s ({len(m.gpus)} GPUs"
          f"{'' if m.psutil else ', no psutil: no CPU, RAM or disk'})", file=sys.stderr, flush=True)
    failing = False
    while True:
        start = time.monotonic()
        try:
            request("POST", url, key, m.read())
            if failing:
                print("runyard agent: sending again", file=sys.stderr, flush=True)
            failing = False
        except Exception as e:
            if once:
                raise
            if not failing:
                print(f"runyard agent: {e}; retrying", file=sys.stderr, flush=True)
            failing = True
        if once:
            return
        time.sleep(max(0.0, interval - (time.monotonic() - start)))
