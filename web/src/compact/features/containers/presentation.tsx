import type { ContainerPresentation } from '../../../features/containers/ContainerUI'

export const compactContainerPresentation: ContainerPresentation = {
  tableClassName: 'data-table',
  Button: ({ className = '', type = 'button', ...props }) => <button {...props} type={type}
    className={`${className.includes('ct-danger') ? 'danger-button' : className.includes('ct-primary') ? 'primary-button' : 'toolbar-button'} ${className}`} />,
  Surface: ({ as: Element = 'div', className = '', ...props }) => <Element {...props} className={`panel ct-card ${className}`} />,
  Status: ({ status, children }) => <span className={`ct-state ct-state-${status}`}>{children}</span>,
}
