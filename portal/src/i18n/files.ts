import type { Messages } from "@buildmax/gui"

// A Space's working files: the upload bar, folder tree, listing, and viewer.
export const filesMessages = {
  en: {
    "files.title": "Files",
    "files.subtitle":
      "This space's working files — mutable inputs and in-progress state that agent runs read and write. Once work is done, publish a durable, unchanging copy as an Artifact instead.",
    "files.uploadFiles": "Upload Files",
    "files.uploadFolder": "Upload Folder",
    "files.back": "Back",
    "files.root": "home",
    "files.emptyRoot":
      "Nothing uploaded yet. Upload files or a folder above, or have an agent write here during a run.",
    "files.emptyFolder": "(empty)",
    "files.directoryTree": "Directory tree",
    "files.folderTree": "Folder tree",
    "files.fileContent": "File content",
    "files.viewerError": "Error: {message}",
    "files.error.loadTree": "Failed to load files",
    "files.error.loadFile": "Failed to load file",
    "files.upload.notAuthenticated": "Not authenticated",
    "files.upload.tooMany": "Too many files (max {max})",
    "files.upload.done": "Uploaded {count} file(s)",
    "files.upload.failed": "Upload failed",
  },
  "zh-CN": {
    "files.title": "文件",
    "files.subtitle":
      "这个 Space 的工作文件：Agent 运行时读写的可变输入和进行中的状态。工作完成后，应改为发布一份持久、不可变的副本作为 Artifact。",
    "files.uploadFiles": "上传文件",
    "files.uploadFolder": "上传文件夹",
    "files.back": "返回",
    "files.root": "根目录",
    "files.emptyRoot": "还没有上传任何内容。可在上方上传文件或文件夹，或让 Agent 在运行时写入这里。",
    "files.emptyFolder": "（空）",
    "files.directoryTree": "目录树",
    "files.folderTree": "文件夹树",
    "files.fileContent": "文件内容",
    "files.viewerError": "错误：{message}",
    "files.error.loadTree": "加载文件失败",
    "files.error.loadFile": "加载文件内容失败",
    "files.upload.notAuthenticated": "未登录",
    "files.upload.tooMany": "文件过多（最多 {max} 个）",
    "files.upload.done": "已上传 {count} 个文件",
    "files.upload.failed": "上传失败",
  },
} satisfies Messages<string>
