/*
Copyright (C) 2025 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

import React from 'react';
import { Modal } from '@douyinfe/semi-ui';

const ROLE_GROUP_ADMIN = 5;
const ROLE_ADMIN = 10;

const DemoteUserModal = ({ visible, onCancel, onConfirm, user, t }) => {
  let targetRoleName = '';
  if (user) {
    if (user.role >= ROLE_ADMIN) {
      targetRoleName = t('组内管理员');
    } else if (user.role === ROLE_GROUP_ADMIN) {
      targetRoleName = t('普通用户');
    }
  }

  return (
    <Modal
      title={t('确定要降级此用户吗？')}
      visible={visible}
      onCancel={onCancel}
      onOk={onConfirm}
      type='warning'
    >
      <div>
        {t('此操作将降低用户的权限级别')}：{targetRoleName}
      </div>
    </Modal>
  );
};

export default DemoteUserModal;
