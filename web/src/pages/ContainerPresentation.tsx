import type { ContainerPresentation } from '../features/containers/ContainerUI'
import { Badge } from '../ui/Badge'
import { Button } from '../ui/Button'
import { Card } from '../ui/Card'

export const auroraContainerPresentation: ContainerPresentation = {
  tableClassName: 'table',
  Button: ({ className = '', ...props }) => <Button {...props} className={className}
    variant={className.includes('ct-danger') ? 'danger' : className.includes('ct-primary') ? 'primary' : 'ghost'} />,
  Surface: ({ className = '', children, ...props }) => <Card {...props} className={`ct-card ${className}`}>{children}</Card>,
  Status: ({ status, children }) => <Badge dot tone={status === 'running' ? 'ok' : status === 'error' ? 'err'
    : ['starting', 'downloading', 'extracting'].includes(status) ? 'warn' : 'neutral'}>{children}</Badge>,
}
