package remotestats

// The adapter LUID joins DXGI identities to WDDM counters. Device names and
// counter ordering cannot identify an adapter on multi-GPU or remote desktops.
const windowsGPUProbe = `
function Get-GPUAdapters {
    $adapters=@()
    try {
        if (!('MottyGPU' -as [type])) {
            Add-Type -TypeDefinition @'
using System;
using System.Collections.Generic;
using System.Runtime.InteropServices;

[ComImport, Guid("770aae78-f26f-4dba-a829-253c83d1b387"), InterfaceType(ComInterfaceType.InterfaceIsIUnknown)]
interface IMottyFactory {
    [PreserveSig] int SetPrivateData(ref Guid id, uint size, IntPtr data);
    [PreserveSig] int SetPrivateDataInterface(ref Guid id, IntPtr data);
    [PreserveSig] int GetPrivateData(ref Guid id, ref uint size, IntPtr data);
    [PreserveSig] int GetParent(ref Guid id, out IntPtr parent);
    [PreserveSig] int EnumAdapters(uint index, out IntPtr adapter);
    [PreserveSig] int MakeWindowAssociation(IntPtr window, uint flags);
    [PreserveSig] int GetWindowAssociation(out IntPtr window);
    [PreserveSig] int CreateSwapChain(IntPtr device, IntPtr description, out IntPtr chain);
    [PreserveSig] int CreateSoftwareAdapter(IntPtr module, out IntPtr adapter);
    [PreserveSig] int EnumAdapters1(uint index, out IMottyAdapter adapter);
}

[ComImport, Guid("29038f61-3839-4626-91fd-086879011a05"), InterfaceType(ComInterfaceType.InterfaceIsIUnknown)]
interface IMottyAdapter {
    [PreserveSig] int SetPrivateData(ref Guid id, uint size, IntPtr data);
    [PreserveSig] int SetPrivateDataInterface(ref Guid id, IntPtr data);
    [PreserveSig] int GetPrivateData(ref Guid id, ref uint size, IntPtr data);
    [PreserveSig] int GetParent(ref Guid id, out IntPtr parent);
    [PreserveSig] int EnumOutputs(uint index, out IntPtr output);
    [PreserveSig] int GetDesc(IntPtr description);
    [PreserveSig] int CheckInterfaceSupport(ref Guid id, out long version);
    [PreserveSig] int GetDesc1(out MottyAdapterDescription description);
}

[StructLayout(LayoutKind.Sequential, CharSet=CharSet.Unicode)]
struct MottyAdapterDescription {
    [MarshalAs(UnmanagedType.ByValTStr, SizeConst=128)] public string Name;
    public uint Vendor, Device, SubSystem, Revision;
    public UIntPtr VideoMemory, SystemMemory, SharedMemory;
    public uint LuidLow, LuidHigh, Flags;
}

public class MottyGPU {
    public string Name, Luid, DeviceKey;
    [DllImport("dxgi.dll")]
    static extern int CreateDXGIFactory1(ref Guid id, out IMottyFactory factory);

    public static MottyGPU[] Get() {
        var adapters=new List<MottyGPU>();
        Guid id=new Guid("770aae78-f26f-4dba-a829-253c83d1b387");
        IMottyFactory factory;
        if (CreateDXGIFactory1(ref id, out factory)!=0) return adapters.ToArray();
        try {
            for (uint index=0; index<16; index++) {
                IMottyAdapter adapter;
                if (factory.EnumAdapters1(index, out adapter)!=0) break;
                try {
                    MottyAdapterDescription description;
                    if (adapter.GetDesc1(out description)!=0 || (description.Flags & 2)!=0) continue;
                    adapters.Add(new MottyGPU {
                        Name=description.Name,
                        Luid=String.Format("luid_0x{0:X8}_0x{1:X8}", description.LuidHigh, description.LuidLow),
                        DeviceKey=String.Format("PCI\\VEN_{0:X4}&DEV_{1:X4}&SUBSYS_{2:X8}&REV_{3:X2}", description.Vendor, description.Device, description.SubSystem, description.Revision)
                    });
                } finally { Marshal.ReleaseComObject(adapter); }
            }
        } finally { Marshal.ReleaseComObject(factory); }
        return adapters.ToArray();
    }
}
'@
        }
        $adapters=@([MottyGPU]::Get())
    } catch { }
    $controllers=@()
    try { $controllers=@(Query 'Win32_VideoController' | Select-Object -First 16) } catch { }
    return @{devices=$adapters;controllers=$controllers}
}

function Read-GPUs($adapters) {
    $ids=@{}
    foreach ($adapter in $adapters.devices) { $ids[$adapter.Luid]=$true }
    $memory=@{}
    $engines=@{}
    if ($ids.Count -gt 0) {
        try {
            foreach ($counter in (Query 'Win32_PerfFormattedData_GPUPerformanceCounters_GPUAdapterMemory')) {
                if ($counter.Name -notmatch '^(luid_0x[0-9A-F]+_0x[0-9A-F]+)_phys_[0-9]+$') { continue }
                $luid=$Matches[1]
                if ($ids.ContainsKey($luid) -and $null -ne $counter.DedicatedUsage -and $null -ne $counter.SharedUsage) {
                    $memory[$luid]+=[uint64]$counter.DedicatedUsage+[uint64]$counter.SharedUsage
                }
            }
        } catch { }
        try {
            foreach ($counter in (Query 'Win32_PerfFormattedData_GPUPerformanceCounters_GPUEngine')) {
                if ($counter.Name -notmatch '^(?:pid_[0-9]+_)?(luid_0x[0-9A-F]+_0x[0-9A-F]+)_phys_([0-9]+)_eng_([0-9]+)_engtype_') { continue }
                $luid=$Matches[1]
                if (!$ids.ContainsKey($luid) -or $null -eq $counter.UtilizationPercentage) { continue }
                $key=$luid+'|'+$Matches[2]+'|'+$Matches[3]
                if ($engines.ContainsKey($key) -or $engines.Count -lt 4096) {
                    $engines[$key]+=[double]$counter.UtilizationPercentage
                }
            }
        } catch { }
    }
    $usage=@{}
    foreach ($key in $engines.Keys) {
        $luid=$key.Split('|')[0]
        $usage[$luid]=[math]::Min(100,[math]::Max([double]$usage[$luid],[double]$engines[$key]))
    }
    $rows=@()
    $matched=@{}
    foreach ($adapter in $adapters.devices) {
        $luid=$adapter.Luid
        if (!$memory.ContainsKey($luid) -and !$usage.ContainsKey($luid)) { continue }
        $rows+=@{index=$rows.Count;name=$adapter.Name;util=$usage[$luid];memory=$memory[$luid]}
        $matched[$adapter.DeviceKey]++
    }
    foreach ($controller in $adapters.controllers) {
        $key=([string]$controller.PNPDeviceID -split '\\',3)[0..1] -join '\'
        if ($matched[$key] -gt 0) { $matched[$key]--; continue }
        if (!$controller.Name -or $rows.Count -ge 16) { continue }
        $rows+=@{index=$rows.Count;name=[string]$controller.Name;vendor=[string]$controller.AdapterCompatibility}
    }
    return $rows
}
`

type windowsGPUReading struct {
	Index  int
	Name   string
	Vendor string
	Util   *float64
	Memory *uint64
}

func windowsGPUs(cards []windowsGPUReading) []GPU {
	var gpus []GPU
	seen := make(map[int]bool, len(cards))
	for _, card := range cards {
		name := detailText(card.Name, 128)
		if name == "" || card.Index < 0 || card.Index >= MaxGPUs || seen[card.Index] {
			continue
		}
		seen[card.Index] = true
		vendor := detailText(card.Vendor, 64)
		if vendor == "" {
			vendor = gpuVendor(name)
		}
		gpu := GPU{
			Index: card.Index, Name: name, Vendor: vendor,
			UtilPercent: -1, TemperatureC: -1, PowerWatts: -1, PowerLimitWatts: -1, FanPercent: -1,
		}
		if card.Util != nil && validPercent(*card.Util) {
			gpu.UtilPercent = *card.Util
		}
		if card.Memory != nil {
			gpu.MemoryUsedBytes = *card.Memory
			gpu.MemoryKnown = true
		}
		gpus = append(gpus, gpu)
	}
	return gpus
}
