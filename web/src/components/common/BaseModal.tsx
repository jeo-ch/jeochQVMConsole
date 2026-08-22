import { Modal } from '@douyinfe/semi-ui'
import { useMountModalLifecycle } from '@/hooks/useMountModalLifecycle'

interface BaseModalProps extends Omit<React.ComponentProps<typeof Modal>, 'visible' | 'onCancel' | 'afterClose'> {
  visible: boolean
  onClose: () => void
}

export default function BaseModal({
  visible,
  onClose,
  children,
  ...rest
}: BaseModalProps) {
  const { modalVisible, requestClose, afterModalClose } = useMountModalLifecycle(onClose)

  return (
    <Modal
      visible={visible && modalVisible}
      onCancel={requestClose}
      afterClose={afterModalClose}
      {...rest}
    >
      {children}
    </Modal>
  )
}