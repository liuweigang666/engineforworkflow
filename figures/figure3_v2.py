import matplotlib.pyplot as plt
import numpy as np

# Data source: paper Table 2 (tab:latency) medians, fine-grained-clock
# re-measurement after the J2.2 word-codec change (2026-08-07, Windows QPC,
# 100 ns resolution). Monolithic per-operation medians are batch-mean values
# (the per-operation samples are quantized to the 100 ns clock tick).
stages = ['T1', 'T2', 'T3', 'T4(100)', 'T4(1k)', 'T4(10k)', 'T5']
monolithic = [0.003, 0.030, 0.044, 0.361, 5.550, 73.941, 0.032]
engine = [3.979, 2.527, 5.644, 1.970, 7.521, 77.773, 2.783]
overhead = [3.976, 2.497, 5.600, 1.609, 1.971, 3.832, 2.751]

x = np.arange(len(stages))
width = 0.35

fig, ax = plt.subplots(figsize=(7.5, 4))

# Academic palette matching the original figure (seaborn-v0_8 muted).
color_mono = '#BBBBBB'  # neutral gray baseline
color_eng = '#4C72B0'   # academic blue (engine)
color_text = '#DD8452'  # orange overhead labels

rects1 = ax.bar(x - width / 2, monolithic, width, label='Monolithic (Baseline)',
                color=color_mono, edgecolor='black', linewidth=0.8)
rects2 = ax.bar(x + width / 2, engine, width, label='Workflow Engine (Proposed)',
                color=color_eng, edgecolor='black', linewidth=0.8)

# Overhead labels above engine bars.
for i in range(len(stages)):
    ax.text(i + width / 2, engine[i] * 1.15, f"+{overhead[i]:g}",
            ha='center', va='bottom', color=color_text, fontsize=9, fontweight='bold')

# TDMA slot reference.
ax.axhline(y=7812.5, color='red', linestyle='--', linewidth=1, alpha=0.5)
# Label below the dashed line, kept inside the plot frame.
ax.text(6.15, 7812.5 * 0.72, '7.8125 ms TDMA slot', fontsize=9,
        color='red', alpha=0.6, ha='center', va='top')

ax.set_yscale('log')
ax.set_ylabel('Execution Latency ($\\mu$s)', fontsize=11)
ax.set_xticks(x)
ax.set_xticklabels(stages)
ax.legend(frameon=False, loc='upper left', bbox_to_anchor=(0, 0.85))
ax.grid(axis='y', which='major', linestyle='--', alpha=0.3)

plt.title('Figure 3. Comparative Analysis of Execution Overhead', fontsize=12)
plt.tight_layout()
plt.savefig('Figure3_v2.pdf')
print('saved Figure3_v2.pdf')
