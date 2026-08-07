import matplotlib.pyplot as plt
import numpy as np

# Data source: paper Table 4 (tab:throughput) + Table 5 (tab:capacity) and
# the per-message P99 tail latency measured by cmd/throughput with the
# fine-grained clock (2026-08-03).
# Both configurations process 100% of offered messages at every rate.
input_rates = [100, 500, 1000, 2000, 5000, 8000, 10000]
independent = [100, 500, 1000, 2000, 5000, 8000, 10000]
shared = [100, 500, 1000, 2000, 5000, 8000, 10000]
# All runs processed exactly the offered count (zero drops), so no error bars.
lat_p99 = [417.62, 571.02, 663.80, 815.80, 900.78, 885.30, 887.94]

x = np.arange(len(input_rates))

fig, (ax1, ax2) = plt.subplots(2, 1, figsize=(7, 4), sharex=True)
plt.subplots_adjust(hspace=0.1)

# Top: processed rate per offered rate (independent blue, shared gray).
ax1.bar(x - 0.2, independent, color='#4C72B0', alpha=0.85, width=0.4,
        edgecolor='black', linewidth=0.8, label='Independent-db')
ax1.bar(x + 0.2, shared, color='#BBBBBB', alpha=0.85, width=0.4,
        edgecolor='black', linewidth=0.8, label='Shared-db')
ax1.set_ylabel('Processed Rate (msg/s)', fontsize=10, fontweight='bold')
ax1.grid(axis='y', linestyle='--', alpha=0.3)
ax1.set_title('Figure 4. Stress Test Performance Analysis', fontsize=12)
ax1.legend(frameon=False, loc='upper left')

# Bottom: P99 per-message tail latency.
ax2.plot(x, lat_p99, color='#DD8452', marker='s', markersize=6, linewidth=2,
         label='P99 Tail Latency (independent-db)')
ax2.set_xlabel('Message Input Rate (msg/s)', fontsize=11)
ax2.set_ylabel('P99 Latency ($\\mu$s)', fontsize=10, fontweight='bold')
ax2.grid(axis='y', linestyle='--', alpha=0.3)
ax2.legend(frameon=False, loc='upper left')

plt.xticks(x, ['100', '500', '1k', '2k', '5k', '8k', '10k'])
plt.tight_layout()
plt.savefig('Figure4_v2.pdf')
print('saved Figure4_v2.pdf')
