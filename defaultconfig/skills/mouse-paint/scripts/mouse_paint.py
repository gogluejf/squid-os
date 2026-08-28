#!/usr/bin/env python3
"""Paint block text and symbols by controlling the system pointer."""
from __future__ import annotations

import argparse
import os
import platform
import re
import shutil
import subprocess
import sys
import time
from dataclasses import dataclass

FONT = {
    "A":("01110","10001","10001","11111","10001","10001","10001"), "B":("11110","10001","10001","11110","10001","10001","11110"),
    "C":("01111","10000","10000","10000","10000","10000","01111"), "D":("11110","10001","10001","10001","10001","10001","11110"),
    "E":("11111","10000","10000","11110","10000","10000","11111"), "F":("11111","10000","10000","11110","10000","10000","10000"),
    "G":("01111","10000","10000","10111","10001","10001","01111"), "H":("10001","10001","10001","11111","10001","10001","10001"),
    "I":("11111","00100","00100","00100","00100","00100","11111"), "J":("00111","00010","00010","00010","10010","10010","01100"),
    "K":("10001","10010","10100","11000","10100","10010","10001"), "L":("10000","10000","10000","10000","10000","10000","11111"),
    "M":("10001","11011","10101","10101","10001","10001","10001"), "N":("10001","11001","10101","10011","10001","10001","10001"),
    "O":("01110","10001","10001","10001","10001","10001","01110"), "P":("11110","10001","10001","11110","10000","10000","10000"),
    "Q":("01110","10001","10001","10001","10101","10010","01101"), "R":("11110","10001","10001","11110","10100","10010","10001"),
    "S":("01111","10000","10000","01110","00001","00001","11110"), "T":("11111","00100","00100","00100","00100","00100","00100"),
    "U":("10001","10001","10001","10001","10001","10001","01110"), "V":("10001","10001","10001","10001","10001","01010","00100"),
    "W":("10001","10001","10001","10101","10101","10101","01010"), "X":("10001","10001","01010","00100","01010","10001","10001"),
    "Y":("10001","10001","01010","00100","00100","00100","00100"), "Z":("11111","00001","00010","00100","01000","10000","11111"),
    ".":("00000","00000","00000","00000","00000","00110","00110"), ",":("00000","00000","00000","00000","00110","00110","00100"),
    "!":("00100","00100","00100","00100","00100","00000","00100"), "?":("01110","10001","00001","00010","00100","00000","00100"),
    "'":("00100","00100","00000","00000","00000","00000","00000"), "-":("00000","00000","00000","11111","00000","00000","00000"),
    ":":("00000","00100","00100","00000","00100","00100","00000"), "/":("00001","00010","00010","00100","01000","01000","10000"),
    "0":("01110","10001","10011","10101","11001","10001","01110"), "1":("00100","01100","00100","00100","00100","00100","01110"),
    "2":("01110","10001","00001","00010","00100","01000","11111"), "3":("11110","00001","00001","01110","00001","00001","11110"),
    "4":("00010","00110","01010","10010","11111","00010","00010"), "5":("11111","10000","10000","11110","00001","00001","11110"),
    "6":("01110","10000","10000","11110","10001","10001","01110"), "7":("11111","00001","00010","00100","01000","01000","01000"),
    "8":("01110","10001","10001","01110","10001","10001","01110"), "9":("01110","10001","10001","01111","00001","00001","01110"),
}

SYMBOLS = {
    "heart":("01100110","11111111","11111111","11111111","01111110","00111100","00011000"),
    "diamond":("00010000","00111000","01111100","11111110","01111100","00111000","00010000"),
    "club":("00011000","00111100","00111100","11011011","11111111","01111110","00011000","00111100"),
    "spade":("00011000","00111100","01111110","11111111","11111111","11011011","00011000","00111100"),
    "squid":(
        "00000111100000","00011000011000","00100000000100","01000000110010","01000000110010","10000000001001",
        "10000000000001","10000100100001","10000100100001","01100000000110","00011000011000","00000111100000",
        "00000000000000","00000111100000","00011111111000","00111111111100","00100110010110","00110010010010",
        "00010011010011","00010011010011","10001001001001","01001001000001","00110010000010","00000000000100",
    ),
}


class DependencyError(RuntimeError):
    pass


class Mouse:
    name = "unknown"
    def position(self): raise NotImplementedError
    def move(self, x, y): raise NotImplementedError
    def down(self): raise NotImplementedError
    def up(self): raise NotImplementedError


class X11Mouse(Mouse):
    name = "Linux X11 / xdotool"
    def __init__(self):
        if not shutil.which("xdotool"):
            raise DependencyError(linux_install_advice("xdotool"))
    def _run(self, *args, capture=False):
        return subprocess.run(["xdotool", *map(str,args)], check=True, capture_output=capture, text=True)
    def position(self):
        out = self._run("getmouselocation", capture=True).stdout
        m = re.search(r"x:(-?\d+)\s+y:(-?\d+)", out)
        if not m: raise RuntimeError("Could not read the X11 pointer position")
        return int(m.group(1)), int(m.group(2))
    def move(self,x,y): self._run("mousemove",int(x),int(y))
    def down(self): self._run("mousedown",1)
    def up(self): self._run("mouseup",1)


class WaylandMouse(Mouse):
    name = "Linux Wayland / ydotool"
    def __init__(self):
        if not shutil.which("ydotool"):
            raise DependencyError(linux_install_advice("ydotool") + "\nThen start ydotoold and grant access to /dev/uinput.")
    def _run(self,*args): subprocess.run(["ydotool",*map(str,args)],check=True)
    def position(self):
        raise RuntimeError("Wayland does not expose the global pointer position. Invoke with --at X,Y.")
    def move(self,x,y): self._run("mousemove","--absolute","-x",int(x),"-y",int(y))
    def down(self): self._run("click","0x40")
    def up(self): self._run("click","0x80")


class WindowsMouse(Mouse):
    name = "Windows / native user32"
    def __init__(self):
        import ctypes
        self.ctypes = ctypes
        self.user32 = ctypes.windll.user32
    def position(self):
        class Point(self.ctypes.Structure): _fields_=[("x",self.ctypes.c_long),("y",self.ctypes.c_long)]
        p=Point()
        if not self.user32.GetCursorPos(self.ctypes.byref(p)): raise RuntimeError("GetCursorPos failed")
        return p.x,p.y
    def move(self,x,y):
        if not self.user32.SetCursorPos(int(x),int(y)): raise RuntimeError("SetCursorPos failed")
    def down(self): self.user32.mouse_event(0x0002,0,0,0,0)
    def up(self): self.user32.mouse_event(0x0004,0,0,0,0)


class MacMouse(Mouse):
    name = "macOS / Quartz"
    def __init__(self):
        try:
            import Quartz
        except ImportError as exc:
            raise DependencyError("Missing Quartz bindings. Run: python3 -m pip install pyobjc-framework-Quartz\nThen grant Accessibility permission to Terminal/Python in System Settings.") from exc
        self.q=Quartz
        self.pressed=False
    def position(self):
        p=self.q.CGEventGetLocation(self.q.CGEventCreate(None)); return int(p.x),int(p.y)
    def _event(self,event_type,x,y):
        event=self.q.CGEventCreateMouseEvent(None,event_type,(x,y),self.q.kCGMouseButtonLeft)
        self.q.CGEventPost(self.q.kCGHIDEventTap,event)
    def move(self,x,y):
        event_type = self.q.kCGEventLeftMouseDragged if self.pressed else self.q.kCGEventMouseMoved
        self._event(event_type,int(x),int(y))
    def down(self):
        x,y=self.position(); self._event(self.q.kCGEventLeftMouseDown,x,y); self.pressed=True
    def up(self):
        x,y=self.position(); self._event(self.q.kCGEventLeftMouseUp,x,y); self.pressed=False


def linux_install_advice(package):
    distro=""
    try:
        for line in open("/etc/os-release",encoding="utf-8"):
            if line.startswith("ID="): distro=line.split("=",1)[1].strip().strip('"'); break
    except OSError: pass
    if distro in {"fedora","rhel","centos","rocky","almalinux"}: return f"Missing {package}. Run: sudo dnf install {package}"
    if distro in {"arch","manjaro","endeavouros"}: return f"Missing {package}. Run: sudo pacman -S {package}"
    if distro in {"opensuse","opensuse-leap","opensuse-tumbleweed","sles"}: return f"Missing {package}. Run: sudo zypper install {package}"
    return f"Missing {package}. Run: sudo apt install {package}"


def detect_mouse():
    system=platform.system()
    if system=="Windows": return WindowsMouse()
    if system=="Darwin": return MacMouse()
    if system=="Linux":
        if os.environ.get("WAYLAND_DISPLAY") and not os.environ.get("DISPLAY"): return WaylandMouse()
        if os.environ.get("DISPLAY"): return X11Mouse()
        if os.environ.get("WAYLAND_DISPLAY"): return WaylandMouse()
        raise RuntimeError("No graphical Linux session detected (DISPLAY/WAYLAND_DISPLAY are unset).")
    raise RuntimeError(f"Unsupported operating system: {system}")


@dataclass
class Painter:
    mouse: Mouse
    scale: int = 5
    speed: float = .004
    event_delay: float = .006

    def move(self,x,y): self.mouse.move(round(x),round(y)); time.sleep(self.speed)
    def up(self): self.mouse.up(); time.sleep(self.event_delay)
    def down(self): self.mouse.down(); time.sleep(self.event_delay)
    def stroke(self,x1,y1,x2,y2):
        self.up(); self.move(x1,y1); self.down()
        steps=max(1,int(max(abs(x2-x1),abs(y2-y1))/2))
        for i in range(1,steps+1):
            t=i/steps; self.move(x1+(x2-x1)*t,y1+(y2-y1)*t)
        self.up()
    def bitmap(self,bitmap,x,y):
        for row_i,row in enumerate(bitmap):
            col=0
            while col<len(row):
                if row[col]=="0": col+=1; continue
                start=col
                while col+1<len(row) and row[col+1]=="1": col+=1
                cy=y+row_i*self.scale+self.scale//2
                x1=x+start*self.scale+self.scale//2
                x2=x+col*self.scale+self.scale//2
                if x1==x2: x2+=max(2,self.scale//2)
                self.stroke(x1,cy,x2,cy); col+=1
    def paint(self,text,x,y):
        origin=x; cx=x; cy=y; line_h=7*self.scale+4*self.scale
        tokens=re.split(r"(\[[A-Za-z]+\]|\n)",text)
        for token in tokens:
            if not token: continue
            if token=="\n": cx=origin; cy+=line_h; continue
            symbol=re.fullmatch(r"\[([A-Za-z]+)\]",token)
            if symbol:
                name=symbol.group(1).lower()
                if name not in SYMBOLS: raise ValueError(f"Unknown symbol [{name}]. Available: {', '.join(SYMBOLS)}")
                image=SYMBOLS[name]; self.bitmap(image,cx,cy); cx+=len(image[0])*self.scale+2*self.scale
                continue
            for ch in token:
                if ch==" ": cx+=4*self.scale; continue
                image=FONT.get(ch.upper(),FONT["?"]); self.bitmap(image,cx,cy); cx+=5*self.scale+2*self.scale
        self.up(); return cx,cy


def parse_at(value):
    m=re.fullmatch(r"\s*(-?\d+)\s*,\s*(-?\d+)\s*",value)
    if not m: raise argparse.ArgumentTypeError("position must be X,Y")
    return int(m.group(1)),int(m.group(2))


def parser():
    p=argparse.ArgumentParser(prog="mouse-paint",description="Paint block text and [symbols] in the active canvas from the pointer position.")
    p.add_argument("text",nargs="?",help="text to paint; supports newlines and [heart], [diamond], [club], [spade], [squid]")
    p.add_argument("--at",type=parse_at,metavar="X,Y",help="start at explicit screen coordinates")
    p.add_argument("--size",type=int,default=5,metavar="PIXELS",help="bitmap pixel size (default: 5)")
    p.add_argument("--speed",choices=("fast","normal","slow"),default="fast")
    p.add_argument("--symbols",action="store_true",help="list available symbols")
    p.add_argument("--doctor",action="store_true",help="report platform/backend readiness without painting")
    return p


def main():
    args=parser().parse_args()
    if args.symbols:
        print(" ".join(f"[{name}]" for name in SYMBOLS)); return 0
    try: mouse=detect_mouse()
    except (DependencyError,RuntimeError) as exc:
        print(f"mouse-paint is not ready: {exc}",file=sys.stderr); return 2
    if args.doctor:
        print(f"Ready: {mouse.name}"); return 0
    if args.text is None:
        parser().error("TEXT is required unless --symbols or --doctor is used")
    if args.size<2: parser().error("--size must be at least 2")
    try: start=args.at or mouse.position()
    except RuntimeError as exc:
        print(f"mouse-paint cannot start: {exc}",file=sys.stderr); return 2
    delays={"fast":(.003,.005),"normal":(.008,.01),"slow":(.02,.02)}
    speed,event=delays[args.speed]
    painter=Painter(mouse,args.size,speed,event)
    try:
        end=painter.paint(args.text,*start)
    finally:
        try: mouse.up()
        except Exception: pass
    print(f"Painted from {start[0]},{start[1]} to {end[0]},{end[1]} using {mouse.name}.")
    return 0


if __name__=="__main__":
    raise SystemExit(main())
