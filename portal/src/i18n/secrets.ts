import type { Messages } from "@buildmax/gui"

// A Space's Secrets: the list, the create form, and editing a Secret's items.
export const secretsMessages = {
  en: {
    "secrets.title": "Secrets",
    "secrets.checking": "Checking whether you can manage this space's secrets…",
    "secrets.unverified":
      "Couldn't verify your role in this space, so secrets stay unavailable. Refresh to try again.",
    "secrets.ownerOnly": "Only a space owner can view or manage this space's secrets.",
    "secrets.intro":
      "Credentials this space's agents can use — a GitHub token, an internal API key. Stored encrypted; values are never shown again after you save them.",
    "secrets.new": "New secret",
    "secrets.warning.lead": "An agent you grant a secret to can read its value.",
    // Follows the bold lead sentence; English needs the leading space.
    "secrets.warning.body":
      " It runs commands the model chooses, and a value in its environment can be printed — so anyone who can trigger that agent can obtain the value without owning the secret. Prefer a short-lived, narrowly scoped credential, and don't grant one to an agent you would not hand it to directly.",
    "secrets.error.load": "Failed to load this space's secrets",
    "secrets.empty.title": "No secrets yet",
    "secrets.empty.copy":
      "Add a credential your space's agents can use. Its value is encrypted on save and never shown again.",

    "secrets.field.name": "Name",
    "secrets.field.description": "Description",
    "secrets.field.optional": "optional",
    "secrets.field.descriptionPlaceholder": "What it is for — never the value itself",
    "secrets.field.items": "Items",
    "secrets.rowEditor": "Row editor",
    "secrets.pasteJson": "Paste JSON",
    "secrets.error.jsonShape": "expected a JSON object of string values",
    "secrets.error.invalidJson": "The items are not valid JSON",
    "secrets.error.required": "A secret needs a name and at least one item.",
    "secrets.error.create": "Failed to create the secret",
    "secrets.cancel": "Cancel",
    "secrets.create": "Create secret",

    "secrets.item.name": "Item name",
    "secrets.item.value": "Item value",
    "secrets.item.valuePlaceholder": "value",
    "secrets.item.remove": "Remove item",
    "secrets.item.add": "+ Add item",

    "secrets.state.active": "active",
    "secrets.state.disabled": "disabled",
    "secrets.state.destroyed": "destroyed",
    "secrets.noItems": "no items",
    "secrets.close": "Close",
    "secrets.editItems": "Edit items",
    "secrets.disable": "Disable",
    "secrets.enable": "Enable",
    "secrets.destroy": "Destroy",
    "secrets.destroyConfirm": 'Destroy secret "{name}"? This cannot be undone.',

    "secrets.edit.nothing": "Nothing to change: set an item or mark one to remove.",
    "secrets.edit.error": "Failed to edit the secret's items",
    "secrets.edit.hint": "Values are never shown. Set an item to replace its value, or mark one to remove.",
    "secrets.edit.remove": "Remove",
    "secrets.edit.setOrAdd": "Set or add items",
    "secrets.edit.save": "Save items",
  },
  "zh-CN": {
    "secrets.title": "密钥",
    "secrets.checking": "正在检查你是否可以管理此 Space 的密钥…",
    "secrets.unverified": "无法确认你在此 Space 中的角色，因此密钥暂不可用。请刷新后重试。",
    "secrets.ownerOnly": "只有 Space 所有者可以查看或管理此 Space 的密钥。",
    "secrets.intro":
      "此 Space 的 Agent 可以使用的凭据，例如 GitHub Token、内部 API 密钥。加密存储，保存后不会再显示其值。",
    "secrets.new": "新建密钥",
    "secrets.warning.lead": "被授予密钥的 Agent 可以读取它的值。",
    "secrets.warning.body":
      "Agent 会执行模型选择的命令，环境中的值可能被打印出来——因此任何能触发该 Agent 的人，即使不拥有该密钥，也能获取它的值。请优先使用短期、范围最小的凭据，不要把密钥授予你不愿直接交给它的 Agent。",
    "secrets.error.load": "加载此 Space 的密钥失败",
    "secrets.empty.title": "暂无密钥",
    "secrets.empty.copy": "添加此 Space 的 Agent 可以使用的凭据。其值在保存时加密，之后不会再显示。",

    "secrets.field.name": "名称",
    "secrets.field.description": "描述",
    "secrets.field.optional": "可选",
    "secrets.field.descriptionPlaceholder": "说明用途，切勿填写值本身",
    "secrets.field.items": "条目",
    "secrets.rowEditor": "逐行编辑",
    "secrets.pasteJson": "粘贴 JSON",
    "secrets.error.jsonShape": "需要一个值均为字符串的 JSON 对象",
    "secrets.error.invalidJson": "条目不是有效的 JSON",
    "secrets.error.required": "密钥需要名称和至少一个条目。",
    "secrets.error.create": "创建密钥失败",
    "secrets.cancel": "取消",
    "secrets.create": "创建密钥",

    "secrets.item.name": "条目名称",
    "secrets.item.value": "条目值",
    "secrets.item.valuePlaceholder": "值",
    "secrets.item.remove": "移除条目",
    "secrets.item.add": "+ 添加条目",

    "secrets.state.active": "已启用",
    "secrets.state.disabled": "已停用",
    "secrets.state.destroyed": "已销毁",
    "secrets.noItems": "无条目",
    "secrets.close": "关闭",
    "secrets.editItems": "编辑条目",
    "secrets.disable": "停用",
    "secrets.enable": "启用",
    "secrets.destroy": "销毁",
    "secrets.destroyConfirm": "确定销毁密钥“{name}”？此操作无法撤销。",

    "secrets.edit.nothing": "没有可保存的更改：请设置条目，或标记要移除的条目。",
    "secrets.edit.error": "编辑密钥条目失败",
    "secrets.edit.hint": "值永远不会显示。设置条目会替换其值，也可以标记要移除的条目。",
    "secrets.edit.remove": "移除",
    "secrets.edit.setOrAdd": "设置或添加条目",
    "secrets.edit.save": "保存条目",
  },
} satisfies Messages<string>
