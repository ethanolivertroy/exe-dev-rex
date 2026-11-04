-- exe.dev VMs as Rex hosts: command-palette actions.
-- Each opens a tab that runs exe-new / exe-rm (in ~/.local/bin). The scripts ask
-- for a VM name when none is given, and report progress with OSC 7501, so the
-- tab shows a spinner, then done or failed. The tab stays open afterwards.

local HOME = os.getenv("HOME") or ""

local function exe_tab(label, command)
  local w, err = rex.session.new_window{
    window_label = label,
    layout = rex.layout.block{
      flavor  = "com.superlogical.terminal.shell",
      options = {
        command      = command,
        path_prepend = { HOME .. "/.local/bin" },
        exit         = { on_completion = false },
      },
    },
  }
  if err ~= nil then error(err, 0) end
  return { window_id = w.window_id }
end

rex.action{
  name        = "exe_new",
  title       = "New exe.dev VM…",
  description = "Create an exe.dev VM and add it to the Rex sidebar (Rex restarts once).",
  category    = "exe.dev",
  keywords    = "exe.dev vm virtual machine create new remote host server cloud",
  args        = { name = "string?" },
  run = function(ctx, args)
    local cmd = { HOME .. "/.local/bin/exe-new" }
    if args.name then table.insert(cmd, args.name) end
    return exe_tab("exe.dev: new " .. (args.name or "VM"), cmd)
  end,
}

rex.action{
  name        = "exe_rm",
  title       = "Remove exe.dev VM…",
  description = "Delete an exe.dev VM, its Tailscale device and its Rex host (Rex restarts once).",
  category    = "exe.dev",
  keywords    = "exe.dev vm virtual machine delete remove destroy remote host server cloud",
  args        = { name = "string?" },
  run = function(ctx, args)
    local cmd = { HOME .. "/.local/bin/exe-rm" }
    if args.name then table.insert(cmd, args.name) end
    return exe_tab("exe.dev: remove " .. (args.name or "VM"), cmd)
  end,
}
