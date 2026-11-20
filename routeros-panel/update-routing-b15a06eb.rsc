# LAB ONLY. Synchronous controller update for one identified installation.
# No reboot watchdog; rollback below handles errors while this script runs.
{
:local tag "b15a06eb5db213e908c7d944d314c7db"
:local short [:pick $tag 0 8]
:local owner ("meridian-install:" . $tag)
:local ctlName ("m7ctl" . $short)
:local ctlComment ($owner . "-policy-controller")
:local ctl [/system/script/find where name=$ctlName and comment=$ctlComment]
:local sched [/system/scheduler/find where name=$ctlName and comment=$ctlComment]
:local cName ("m7c" . $short)
:local container [/container/find where name=$cName and comment=$owner]
:local rows [/container/print detail as-value where name=$cName and comment=$owner]
:if ([:len $ctl]!=1 || [:len $sched]!=1 || [/system/scheduler/get [:pick $sched 0] disabled] || [:len $container]!=1 || [:len $rows]!=1 || (($rows->0)->"image-id")!="3f3e5c877983cad18254874e8dc1ed07cb956ae43641af2d753341819e33536c" || [:tostr (($rows->0)->"running")]!="true") do={ :error "M7SIMPLE owner, scheduler or image mismatch" }
:if ([:convert [/system/script/get [:pick $ctl 0] source] from=raw transform=sha512 to=hex]!="2715b8cafa82548e66b20dee25afeb1360fa38c20ca3dd173b511a5fa9f5644176e75b8963e774cb8e730baaeb1ed6773b56a97a6301038ff5fd7d0b68bd364b") do={ :error "M7SIMPLE old controller mismatch" }
:local envName ("m7e" . $short)
:local mountName ("m7m" . $short)
:local env [/container/envs/find where list=$envName and key="M7_READY_ID"]
:local mount [/container/mounts/find where list=$mountName and dst="/state"]
:if ([:len $env]!=1 || [:len $mount]!=1 || [:tostr [/container/envs/get [:pick $env 0] value]]!=$tag || [:tostr [/container/get [:pick $container 0] envlists]]!=$envName || [:tostr [/container/get [:pick $container 0] mountlists]]!=$mountName || [/container/mounts/get [:pick $mount 0] mode]!="rw") do={ :error "M7SIMPLE container state ownership" }
:local mountSrc [:tostr [/container/mounts/get [:pick $mount 0] src]]
:local mountExpected ("m7s" . $short)
:if ($mountSrc!=$mountExpected && $mountSrc!=("/" . $mountExpected)) do={ :error "M7SIMPLE state mount changed" }

:if ([/system/resource/get free-hdd-space]<6291456) do={ :error "M7SIMPLE insufficient free space" }
:local disk ""
:if ([:len [/file/find where name="flash" and type="disk"]]=1) do={ :set disk "flash/" }
:local host ($disk . "m7h" . $short . ".json")
:local layout ($disk . "m7s" . $short . "/policy-layout.json")
:local state ($disk . "m7x" . $short . ".json")
:local hi [/file/find where name=$host]
:local li [/file/find where name=$layout]
:local si [/file/find where name=$state]
:if ([:len $hi]!=1 || [:len $li]!=1 || [:len $si]!=1 || [/file/get [:pick $hi 0] size]>8192 || [/file/get [:pick $si 0] size]>4096) do={ :error "M7SIMPLE state missing" }
:local oldLayout [/file/get [:pick $hi 0] contents]
:local oldState [/file/get [:pick $si 0] contents]
:if ([/file/get [:pick $li 0] contents]!=$oldLayout) do={ :error "M7SIMPLE layout copies differ" }
:local meta [:deserialize from=json value=$oldLayout options=json.no-string-conversion]
:local st [:deserialize from=json value=$oldState options=json.no-string-conversion]
:if (($meta->"tag")!=$tag || ($meta->"mode_tag")!=$short || ($meta->"version")!=1 || ($st->"mode")!="off" || ($st->"phase")!="stable") do={ :error "M7SIMPLE requires stable off" }
:local obj ($meta->"objects")
:local entry [:toid ($obj->"entry")]
:local gentry [:toid ($obj->"gentry")]
:if (![/ip/firewall/mangle/get $entry disabled] || ![/ip/firewall/filter/get $gentry disabled] || [/ip/firewall/mangle/get $entry comment]!=("m7-" . $short . "-entry") || [/ip/firewall/filter/get $gentry comment]!=("m7-" . $short . "-gentry")) do={ :error "M7SIMPLE selector not off" }
:local foreign [/ip/firewall/mangle/find where chain="prerouting" and action="mark-routing" and new-routing-mark="porno" and dst-address-list="porno"]
:if ([:len $foreign]!=1 || [/ip/firewall/mangle/get [:pick $foreign 0] disabled] || [/ip/firewall/mangle/get [:pick $foreign 0] passthrough]) do={ :error "M7SIMPLE existing route rule changed" }
:local saw false
:local after false
:foreach id in=[/ip/firewall/mangle/find where chain="prerouting"] do={ :if ($id=[:pick $foreign 0]) do={ :set saw true }; :if ($id=$entry && $saw) do={ :set after true } }
:if (!$after) do={ :error "M7SIMPLE existing route priority changed" }
:foreach mode in={"black";"white"} do={ :local label ("m7-" . $short . "-coexist-" . $mode); :if ([:len [/ip/firewall/filter/find where comment=$label]]!=0) do={ :error "M7SIMPLE guard already exists" } }
:local backup ("m7ob" . $short)
:local backupLayout ("m7ol" . $short)
:if ([:len [/system/script/find where name=$backup or name=$backupLayout]]>0) do={ :error "M7SIMPLE backup name in use" }
:local fetch [/tool/fetch url="https://raw.githubusercontent.com/JinComputers/meridian-linux/836e2b1e06460eb462e8a0119218feab27b79e42/routeros-panel/controller-coexist-f3a56572.rsc" output=user as-value check-certificate=yes-without-crl http-max-redirect-count=0 idle-timeout=30s]
:local candidate ($fetch->"data")
:if (($fetch->"status")!="finished" || [:len $candidate]>60000 || [:convert $candidate from=raw transform=sha512 to=hex]!="7d4eaf74767653020f52c1057c72050f3e7a1082a9b6fc7b9a726147be21f628542ba8042a252256afa7039d31f63cb6308a6988221aab56bb41eb1f719cbbbc") do={ :error "M7SIMPLE candidate mismatch" }
# Download a pinned receipt repair helper into the already existing private
# directory left by the successful panel update. No network rules change here.
:local updateDir ($disk . "m7s" . $short . "/m7-panel-update")
:local helperPath ($updateDir . "/receipt-repair")
:local readyPath ($updateDir . "/receipt-ready-39fae868")
:if ([:len [/file/find where name=$updateDir]]!=1 || [:len [/file/find where name=$helperPath or name=$readyPath]]!=0) do={ :error "M7SIMPLE receipt helper location" }
:local download [/tool/fetch url="https://raw.githubusercontent.com/JinComputers/meridian-linux/836e2b1e06460eb462e8a0119218feab27b79e42/routeros-panel/receipt-repair-arm64-5266ec31" dst-path=$helperPath as-value check-certificate=yes-without-crl http-max-redirect-count=0 idle-timeout=30s]
:local helper [/file/find where name=$helperPath]
:if (($download->"status")!="finished" || [:len $helper]!=1 || [/file/get [:pick $helper 0] size]!=2728614) do={ :error "M7SIMPLE receipt helper download" }
:local digest "meridian-oci-v1:"
:local offset 0
:while ($offset<2728614) do={
 :local n (2728614-$offset); :if ($n>32768) do={ :set n 32768 }
 :local block ([/file/read file=$helperPath offset=$offset chunk-size=$n as-value]->"data")
 :if ([:len $block]!=$n) do={ :error "M7SIMPLE receipt helper read" }
 :set digest [:convert ($digest . $block) from=raw transform=sha512 to=hex]
 :set offset ($offset+$n)
}
:if ($digest!="6b88f9c73e34cc50485f1d6c76d998b7d739025af31f1e103a6909ab141c3e15d272bfba2490a9271ef65b00f394a34c4eb0f421626368fd8ef0fc29d4350f33") do={ :error "M7SIMPLE receipt helper checksum" }
:local oldCode [/system/script/get [:pick $ctl 0] source]
:local blackID
:local whiteID
:local changed false
:onerror problem in={
 /system/scheduler/disable [:pick $sched 0]
 :for n from=1 to=30 do={ :if ([:len [/system/script/job/find where script=$ctlName]]=0) do={ :break }; :delay 1s }
 :if ([:len [/system/script/job/find where script=$ctlName]]>0 || [/file/get [:pick $si 0] contents]!=$oldState || [/file/get [:pick $hi 0] contents]!=$oldLayout || [/file/get [:pick $li 0] contents]!=$oldLayout) do={ :error "M7SIMPLE state changed" }
 :local b [/system/script/add name=$backup source=$oldCode policy=read comment=($owner . "-simple-controller-backup")]
 :local l [/system/script/add name=$backupLayout source=$oldLayout policy=read comment=($owner . "-simple-layout-backup")]
 :if ([:convert [/system/script/get $b source] from=raw transform=sha512 to=hex]!="2715b8cafa82548e66b20dee25afeb1360fa38c20ca3dd173b511a5fa9f5644176e75b8963e774cb8e730baaeb1ed6773b56a97a6301038ff5fd7d0b68bd364b" || [/system/script/get $l source]!=$oldLayout) do={ :error "M7SIMPLE backup readback" }
 :set changed true
 :foreach mode in={"black";"white"} do={
  :local chain ("m7gb" . $short)
  :local drop [:toid ($obj->"gl-black")]
  :local dropRole "gl-black"
  :if ($mode="white") do={ :set chain ("m7gw" . $short); :set drop [:toid ($obj->"gd-white")]; :set dropRole "gd-white" }
  :if ([/ip/firewall/filter/get $drop comment]!=("m7-" . $short . "-" . $dropRole) || [/ip/firewall/filter/get $drop chain]!=$chain || [/ip/firewall/filter/get $drop action]!="drop") do={ :error "M7SIMPLE guard chain changed" }
  :local role ("coexist-" . $mode)
  :local id [/ip/firewall/filter/add chain=$chain action=return routing-mark="porno" place-before=$drop comment=("m7-" . $short . "-" . $role)]
  :if ($mode="black") do={ :set blackID $id } else={ :set whiteID $id }
  :local row ([/ip/firewall/filter/print as-value where .id=$id]->0)
  :foreach field in={"bytes";"packets";"active";"invalid";"immediate-gw";"inactive";"hw-offloaded";"disabled";"log";"log-prefix"} do={ :set ($row->$field) }
  :set ($obj->$role) $id
  :set ($meta->($role . "-sha")) [:convert [:serialize to=json value=$row options=json.no-string-conversion] from=raw transform=sha512 to=hex]
 }
 :set ($meta->"objects") $obj
 :local newLayout [:serialize to=json value=$meta options=json.no-string-conversion]
 /file/set [:pick $hi 0] contents=$newLayout
 /file/set [:pick $li 0] contents=$newLayout
 :if ([/file/get [:pick $hi 0] contents]!=$newLayout || [/file/get [:pick $li 0] contents]!=$newLayout) do={ :error "M7SIMPLE layout readback" }
 /system/script/set [:pick $ctl 0] source=$candidate
 :if ([:convert [/system/script/get [:pick $ctl 0] source] from=raw transform=sha512 to=hex]!="7d4eaf74767653020f52c1057c72050f3e7a1082a9b6fc7b9a726147be21f628542ba8042a252256afa7039d31f63cb6308a6988221aab56bb41eb1f719cbbbc") do={ :error "M7SIMPLE controller readback" }
 # Receipt repair must finish here while the scheduler remains stopped.
 # The helper URL, digest and output marker are renderer-pinned. The host policy
# scheduler is already disabled and no controller job is running here.
/container/shell [:pick $container 0] cmd="/bin/sh -c \"echo '5266ec31e63ecee9b8055f9e24354a423c3b85b3e5f62d5c38bb5cffa075d098  /state/m7-panel-update/receipt-repair' | /usr/bin/sha256sum -c - && /bin/chmod 700 /state/m7-panel-update/receipt-repair && echo '5266ec31e63ecee9b8055f9e24354a423c3b85b3e5f62d5c38bb5cffa075d098  /state/m7-panel-update/receipt-repair' | /usr/bin/sha256sum -c - && /state/m7-panel-update/receipt-repair b15a06eb5db213e908c7d944d314c7db && umask 077 && printf %s '5266ec31e63ecee9b8055f9e24354a423c3b85b3e5f62d5c38bb5cffa075d098' > /state/m7-panel-update/receipt-ready-39fae868\"" no-sh
:local ready [/file/find where name=$readyPath]
:if ([:len $ready]!=1 || [/file/get [:pick $ready 0] contents]!="5266ec31e63ecee9b8055f9e24354a423c3b85b3e5f62d5c38bb5cffa075d098") do={ :error "M7SIMPLE receipt repair unconfirmed" }
 /system/scheduler/enable [:pick $sched 0]
 :put "M7SIMPLE controller updated; test modes from the panel"
} do={
 :log warning ("M7SIMPLE update failed: " . $problem)
 :onerror rollback in={
  /system/scheduler/disable [:pick $sched 0]
  :if ($changed) do={
   /system/script/set [:pick $ctl 0] source=$oldCode
   /file/set [:pick $hi 0] contents=$oldLayout
   /file/set [:pick $li 0] contents=$oldLayout
   :foreach id in={$blackID;$whiteID} do={ :if ([:typeof $id]="id") do={ :local cm [/ip/firewall/filter/get $id comment]; :if ($cm!=("m7-" . $short . "-coexist-black") && $cm!=("m7-" . $short . "-coexist-white")) do={ :error "M7SIMPLE guard ownership changed" }; /ip/firewall/filter/remove $id } }
  }
  /system/scheduler/enable [:pick $sched 0]
  :put "M7SIMPLE update failed; controller and rules restored"
 } do={ :error ("M7SIMPLE rollback incomplete: " . $rollback) }
}
}
