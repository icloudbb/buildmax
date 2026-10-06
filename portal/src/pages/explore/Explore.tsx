import { FilesPanel } from "../../components/FilesPanel"
import { useT } from "../../i18n"

interface ExploreProps {
  spaceId: string
}

export function Explore({ spaceId }: ExploreProps) {
  const t = useT()
  return (
    <div className="page-explore">
      <h1 className="page-explore__title">{t("files.title")}</h1>
      <p className="page-explore__subtitle">{t("files.subtitle")}</p>
      <FilesPanel spaceId={spaceId} />
    </div>
  )
}
