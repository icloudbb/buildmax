import { PageHeader } from "@buildmax/gui"
import { FilesPanel } from "../../components/FilesPanel"
import { useT } from "../../i18n"

interface ExploreProps {
  spaceId: string
}

export function Explore({ spaceId }: ExploreProps) {
  const t = useT()
  return (
    <div className="page-explore">
      <PageHeader title={t("files.title")} subtitle={t("files.subtitle")} />
      <FilesPanel spaceId={spaceId} />
    </div>
  )
}
