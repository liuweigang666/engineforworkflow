#!/usr/bin/env python
"""Regenerate Figure 3 (median execution time, monolithic vs. engine) as PDF
using ReportLab, mirroring the original matplotlib figure (figure3_v2.py):
grouped log-scale bars, orange overhead labels, and the 7.8125 ms TDMA-slot
reference line. Data: paper Table 2 (tab:latency) medians after the J2.2
word-codec change (2026-08-07, Windows QPC).

Outputs:
  Figure3_v2.pdf  (this directory, repository copy)
  Figure3.pdf     (paper figures directory, overwritten)
"""

import math
import os

from reportlab.lib.colors import HexColor
from reportlab.lib.pagesizes import letter
from reportlab.pdfgen import canvas

stages = ["T1", "T2", "T3", "T4(100)", "T4(1k)", "T4(10k)", "T5"]
monolithic = [0.003, 0.030, 0.044, 0.361, 5.550, 73.941, 0.032]
engine = [3.979, 2.527, 5.644, 1.970, 7.521, 77.773, 2.783]
overhead = [3.976, 2.497, 5.600, 1.609, 1.971, 3.832, 2.751]

COLOR_MONO = HexColor("#BBBBBB")
COLOR_ENG = HexColor("#4C72B0")
COLOR_TEXT = HexColor("#DD8452")
COLOR_RED = HexColor("#FF0000")

PAGE_W, PAGE_H = 540, 288
LEFT, RIGHT, BOTTOM, TOP = 50, 8, 34, 18
PLOT_W = PAGE_W - LEFT - RIGHT
PLOT_H = PAGE_H - BOTTOM - TOP
LOG_MIN, LOG_MAX = 1e-3, 1e4


def log_y(value):
    return BOTTOM + (math.log10(value) - math.log10(LOG_MIN)) / (
        math.log10(LOG_MAX) - math.log10(LOG_MIN)
    ) * PLOT_H


def make_figure(path):
    c = canvas.Canvas(path, pagesize=(PAGE_W, PAGE_H))
    c.setTitle("Figure 3. Comparative Analysis of Execution Overhead")

    # Grid + y-axis tick labels (log major ticks).
    ticks = [1e-2, 1e-1, 1e0, 1e1, 1e2, 1e3, 1e4]
    labels = ["0.01", "0.1", "1", "10", "100", "1,000", "10,000"]
    c.setStrokeColor(HexColor("#CCCCCC"))
    c.setLineWidth(0.5)
    c.setDash(3, 3)
    for v, lab in zip(ticks, labels):
        y = log_y(v)
        c.line(LEFT, y, LEFT + PLOT_W, y)
        c.setDash()
        c.setFillColor(HexColor("#333333"))
        c.setFont("Helvetica", 8)
        c.drawRightString(LEFT - 5, y - 3, lab)
        c.setDash(3, 3)
    c.setDash()

    # TDMA slot reference.
    slot_y = log_y(7812.5)
    c.setStrokeColor(COLOR_RED)
    c.setLineWidth(1)
    c.setDash(4, 3)
    c.line(LEFT, slot_y, LEFT + PLOT_W, slot_y)
    c.setDash()
    c.setFillColor(COLOR_RED)
    c.setFont("Helvetica", 9)
    c.drawRightString(LEFT + PLOT_W - 2, slot_y + 3, "7.8125 ms TDMA slot")

    # Axes frame.
    c.setStrokeColor(HexColor("#000000"))
    c.setLineWidth(0.8)
    c.rect(LEFT, BOTTOM, PLOT_W, PLOT_H, stroke=1, fill=0)

    # Bars.
    n = len(stages)
    group_w = PLOT_W / n
    bar_w = 20.0
    for i in range(n):
        cx = LEFT + group_w * (i + 0.5)
        for x, val, color in (
            (cx - 11, monolithic[i], COLOR_MONO),
            (cx + 11, engine[i], COLOR_ENG),
        ):
            y = log_y(val)
            c.setFillColor(color)
            c.setStrokeColor(HexColor("#000000"))
            c.setLineWidth(0.8)
            c.rect(x - bar_w / 2, BOTTOM, bar_w, y - BOTTOM, stroke=1, fill=1)

    # Overhead labels above engine bars.
    c.setFillColor(COLOR_TEXT)
    c.setFont("Helvetica-Bold", 9)
    for i in range(n):
        cx = LEFT + group_w * (i + 0.5) + 11
        ly = log_y(engine[i] * 1.18)
        if ly > BOTTOM + PLOT_H - 4:
            ly = BOTTOM + PLOT_H - 4
        c.drawCentredString(cx, ly, "+%g" % overhead[i])

    # X tick labels.
    c.setFillColor(HexColor("#000000"))
    c.setFont("Helvetica", 9)
    for i, s in enumerate(stages):
        cx = LEFT + group_w * (i + 0.5)
        c.drawCentredString(cx, BOTTOM - 14, s)

    # Y-axis label.
    c.saveState()
    c.translate(14, BOTTOM + PLOT_H / 2)
    c.rotate(90)
    c.setFont("Helvetica", 10)
    c.drawCentredString(0, 0, "Execution Latency (\u03bcs)")
    c.restoreState()

    # Title.
    c.setFont("Helvetica-Bold", 11)
    c.drawCentredString(PAGE_W / 2, PAGE_H - 10, "Figure 3. Comparative Analysis of Execution Overhead")

    # Legend.
    lx, ly = LEFT + 8, BOTTOM + PLOT_H - 10
    for label, color in (("Monolithic (Baseline)", COLOR_MONO), ("Workflow Engine (Proposed)", COLOR_ENG)):
        c.setFillColor(color)
        c.setStrokeColor(HexColor("#000000"))
        c.setLineWidth(0.6)
        c.rect(lx, ly - 9, 12, 8, stroke=1, fill=1)
        c.setFillColor(HexColor("#000000"))
        c.setFont("Helvetica", 8)
        c.drawString(lx + 16, ly - 8, label)
        ly -= 13

    c.showPage()
    c.save()
    return path


if __name__ == "__main__":
    here = os.path.dirname(os.path.abspath(__file__))
    repo_out = os.path.join(here, "Figure3_v2.pdf")
    paper_out = os.path.normpath(
        os.path.join(here, "..", "..", "..", "【00】paper_for_submission", "workflow_engine", "figures", "Figure3.pdf")
    )
    for p in (repo_out, paper_out):
        os.makedirs(os.path.dirname(p), exist_ok=True)
        make_figure(p)
        print("saved", p)
